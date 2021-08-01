package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	config "github.com/TRON-US/go-btfs-config"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	ic "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/tron-us/go-btfs-common/crypto"
	"github.com/tron-us/go-btfs-common/ledger"
	escrowpb "github.com/tron-us/go-btfs-common/protos/escrow"
	ledgerpb "github.com/tron-us/go-btfs-common/protos/ledger"
	"github.com/tron-us/go-btfs-common/utils/grpc"
	"github.com/tron-us/protobuf/proto"
	"io"
	"ledger-pay/util"
	"ledger-pay/wallet"
	"net/http"
	"os"
	"strconv"
)

var escrowService = "https://escrow.btfs.io"
var taxLedger []byte
var taxPercent int64 = 5

// TODO 5%
func main() {
	taxLedger, _ = hex.DecodeString("04f431a621b0e56d236fb55651c568724e32716afc9125824ebb3d98889e1364ab5bb9e2b00d916c09dfb873a76b9797fcaee0589256bb2418d8bd4b0d702b06e8")

	r := mux.NewRouter()
	r.HandleFunc("/transfer", transfer)
	//r.HandleFunc("/balance", balance)
	r.HandleFunc("/", info)

	http.Handle("/", r)
	fmt.Println("Port: 30080")
	err := http.ListenAndServe(":30080", nil)
	if err != nil {
		fmt.Printf("Error ListenAndServe: %s\n", err)
		os.Exit(1)
	}
}

func transfer(w http.ResponseWriter, r *http.Request) {
	var err error
	requestId := uuid.New().String()
	fmt.Println("Start transfer:", requestId)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "Request ID: "+requestId+"\n")
	q := r.URL.Query()

	// FROM
	var payerKey *ecdsa.PrivateKey
	var payerLedger []byte
	var payerIdentity config.Identity
	if q.Get("fromKey") != "" {
		var err error
		payerKey, payerLedger, payerIdentity, err = getKeys(q.Get("fromKey"), "secp256k1", "")
		if err != nil {
			fmt.Println(requestId, "Error fromKey:", err)
			io.WriteString(w, "Error fromKey: "+err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	if q.Get("fromSeed") != "" {
		var err error
		payerKey, payerLedger, payerIdentity, err = getKeys("", "BIP39", q.Get("fromSeed"))
		if err != nil {
			fmt.Println(requestId, "Error fromSeed:", err)
			io.WriteString(w, "Error fromSeed: "+err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	// TO (ledger)
	var recipientLedger []byte
	if q.Get("toKey") != "" {
		var err error
		_, recipientLedger, _, err = getKeys(q.Get("toKey"), "secp256k1", "")
		if err != nil {
			fmt.Println(requestId, "Error toKey:", err)
			io.WriteString(w, "Error toKey: "+err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	if q.Get("toSeed") != "" {
		var err error
		_, recipientLedger, _, err = getKeys("", "BIP39", q.Get("toSeed"))
		if err != nil {
			fmt.Println(requestId, "Error toSeed:", err)
			io.WriteString(w, "Error toSeed: "+err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	if q.Get("toSpeed") != "" {
		var err error
		recipientLedger, err = hex.DecodeString(q.Get("toSpeed"))
		if err != nil {
			fmt.Println(requestId, "Error toSpeed:", err)
			io.WriteString(w, "Error toSpeed: "+err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	payerBalanceBefore, err := getInAppBalance(&payerIdentity)
	if err != nil {
		fmt.Println(requestId, "Error getInAppBalance:", err)
		io.WriteString(w, "Error getInAppBalance: "+err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	io.WriteString(w, "Balance before: "+strconv.FormatInt(payerBalanceBefore, 10)+"\n")

	if q.Get("amount") != "" {
		amount, err := strconv.ParseInt(q.Get("amount"), 10, 64)
		if err != nil {
			fmt.Println(requestId, "Error amount:", err)
			io.WriteString(w, "Error amount: "+err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if amount > payerBalanceBefore {
			io.WriteString(w, "Low balance!")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		payerBalanceBefore = amount
	}

	taxSum := payerBalanceBefore * taxPercent / 100
	if taxSum == 0 {
		taxSum += 1
	}
	paySum := payerBalanceBefore - taxSum
	if paySum == 0 {
		io.WriteString(w, "Low amount, min: 2")
		return
	}

	_, err = pay(payerKey, payerLedger, recipientLedger, paySum)
	payInfo, err := pay(payerKey, payerLedger, taxLedger, taxSum)
	if err != nil {
		fmt.Println(requestId, "Error transfer:", err)
		io.WriteString(w, "Error transfer: "+err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	io.WriteString(w, "Transfer sum: "+strconv.FormatInt(paySum, 10)+"\n")
	io.WriteString(w, "Tax sum: "+strconv.FormatInt(taxSum, 10)+"\n")
	io.WriteString(w, payInfo)

	// Составляем ответ. Балансы до, балансы после, сумма перевода и комиссии, адрес кошелька трон и спида

	w.WriteHeader(http.StatusOK)
	fmt.Println("End transfer:", requestId)
}

func getInAppBalance(identity *config.Identity) (int64, error) {
	privKey, err := identity.DecodePrivateKey("")
	if err != nil {
		return 0, err
	}
	lgSignedPubKey, err := ledger.NewSignedPublicKey(privKey, privKey.GetPublic())

	var balance int64 = 0
	err = grpc.EscrowClient(escrowService).WithContext(context.Background(),
		func(ctx context.Context, client escrowpb.EscrowServiceClient) error {
			res, err := client.BalanceOf(ctx, ledger.NewSignedCreateAccountRequest(lgSignedPubKey.Key, lgSignedPubKey.Signature))
			if err != nil {
				return err
			}
			balance = res.Result.Balance
			return nil
		})
	if err != nil {
		return 0, err
	}

	return balance, nil
}

func pay(payerKey *ecdsa.PrivateKey, payerLedger []byte, recipientLedger []byte, amount int64) (payInfo string, err error) {
	transferRequest := &ledgerpb.TransferRequest{
		Payer:     &ledgerpb.PublicKey{Key: payerLedger},
		Recipient: &ledgerpb.PublicKey{Key: recipientLedger},
		Amount:    amount,
	}

	raw, err := proto.Marshal(transferRequest)
	if err != nil {
		return "", err
	}

	signature, err := wallet.SignChannel(raw, payerKey)
	if err != nil {
		return "", err
	}

	request := &ledgerpb.SignedTransferRequest{
		TransferRequest: transferRequest,
		Signature:       signature,
	}

	err = grpc.EscrowClient(escrowService).WithContext(context.Background(),
		func(ctx context.Context, client escrowpb.EscrowServiceClient) error {
			response, err := client.Pay(ctx, request)
			if err != nil {
				return err
			}
			if response == nil {
				return fmt.Errorf("escrow reponse is nil")
			}
			payInfo = "Balance after:" + strconv.FormatInt(response.Balance, 10) + "\n"
			return nil
		})
	if err != nil {
		return "", err
	}

	return payInfo, nil
}

func getKeys(importKey string, keyType string, seedPhrase string) (key *ecdsa.PrivateKey, ledgerAddress []byte, identity config.Identity, err error) {
	k, _, err := util.GenerateKey(importKey, keyType, seedPhrase)

	ks, err := crypto.FromPrivateKey(k)
	if err != nil {
		return nil, nil, config.Identity{}, err
	}

	k64, err := crypto.Hex64ToBase64(ks.HexPrivateKey)
	if err != nil {
		return nil, nil, config.Identity{}, err
	}
	identity.PrivKey = k64

	// get key
	privKeyIC, err := identity.DecodePrivateKey("")
	if err != nil {
		return nil, nil, config.Identity{}, err
	}
	// base64 key
	privKeyRaw, err := privKeyIC.Raw()
	if err != nil {
		return nil, nil, config.Identity{}, err
	}
	// hex key
	hexPrivKey := hex.EncodeToString(privKeyRaw)

	// hex key to ecdsa
	privateKey, err := crypto.HexToECDSA(hexPrivKey)
	if err != nil {
		return nil, nil, config.Identity{}, err
	}
	if privateKey == nil {
		fmt.Println("wallet get private key ecdsa failed")
		return nil, nil, config.Identity{}, err
	}

	ledgerAddress, err = ic.RawFull(privKeyIC.GetPublic())
	if err != nil {
		fmt.Println("get ledger address failed, ERR: \n", err)
		return nil, nil, config.Identity{}, err
	}

	return privateKey, ledgerAddress, identity, nil
}

func balance(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func info(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8") // normal header
	w.WriteHeader(http.StatusOK)

	io.WriteString(w, "Сервис переводов между IN APP балансами! Комиссия 5%\n\n")

	io.WriteString(w, "Для перевода необходимо указать:\n")
	io.WriteString(w, "Кошелёк списания, один из параметров:\n")
	io.WriteString(w, "	fromKey - секретный ключ, пример: 7eb6948762712c08a1ff079dcdf8948e7e9fc9844ca9f619e770ed1fdd83ecf2\n")
	io.WriteString(w, "	fromSeed - 12 слов, пример: muffin,elbow,monster,regular,burger,lady,thrive,virtual,curve,mammal,reflect,venue\n\n")

	io.WriteString(w, "Кошелёк начисления, один из параметров:\n")
	io.WriteString(w, "	toSpeed - адрес кошелька speed/btfs, пример: 04200cf458cefe3c008fa40b4d44a2afbde9a90e64ef4254fbfbe2acccf6cded18711072e54182e7744db421eeab3a34ff0f215beac22db313eb48550e709fbc23\n")
	io.WriteString(w, "	toKey - секретный ключ, пример: 7eb6948762712c08a1ff079dcdf8948e7e9fc9844ca9f619e770ed1fdd83ecf2\n")
	io.WriteString(w, "	toSeed - 12 слов, пример: muffin,elbow,monster,regular,burger,lady,thrive,virtual,curve,mammal,reflect,venue\n\n")

	io.WriteString(w, "Сумма списания, если сумма не указана то спишет весь баланс:\n")
	io.WriteString(w, "	amount - сумма перевода, минимальное знаение 2, 1 БТТ = 1000000\n\n")

	io.WriteString(w, "В итоге у вас должна выйти примерно такая строка, в зависимости от выбора способа указания кошелька:\n")
	io.WriteString(w, "http://127.0.0.1:30080/transfer?fromSeed=muffin,elbow,monster,regular,burger,lady,thrive,virtual,curve,mammal,reflect,venue&toKey=7eb6948762712c08a1ff079dcdf8948e7e9fc9844ca9f619e770ed1fdd83ecf2&amount=2\n")

}
