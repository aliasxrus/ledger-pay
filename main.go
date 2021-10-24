package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

// TODO 5%
func main() {
	taxLedger, _ = base64.StdEncoding.DecodeString("BCKKQghIx/dH3WqaS8+v5ggHEheoaSPGnWre4fxN2jasEI/quxPdtnERBL+OITZD7BMbr0fi9B7j2JAJsNKFVzw=")

	r := mux.NewRouter()
	r.HandleFunc("/transfer", transfer)
	r.HandleFunc("/tm", tm).Methods("POST")
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

type TmReq struct {
	From     string `json:"from"`
	To       string `json:"to"`
	FromType string `json:"fromType"`
	ToType   string `json:"toType"`
}

type TmRes struct {
	Before   int64  `json:"before"`
	After    int64  `json:"after"`
	Transfer int64  `json:"transfer"`
	Tax      int64  `json:"tax"`
	Error    string `json:"error"`
}

func tm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var tmReq TmReq
	var tmRes TmRes

	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&tmReq)
	if err != nil {
		w.WriteHeader(http.StatusTeapot)
		return
	}

	// FROM
	var payerKey *ecdsa.PrivateKey
	var payerLedger []byte
	var payerIdentity config.Identity
	if tmReq.FromType == "key" {
		var err error
		payerKey, payerLedger, payerIdentity, err = getKeys(tmReq.From, "secp256k1", "")
		if err != nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
	}

	if tmReq.FromType == "seed" {
		var err error
		payerKey, payerLedger, payerIdentity, err = getKeys("", "BIP39", tmReq.From)
		if err != nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
	}

	if payerKey == nil {
		w.WriteHeader(http.StatusTeapot)
		return
	}

	// TO (ledger)
	var recipientLedger []byte
	if tmReq.ToType == "key" {
		var err error
		_, recipientLedger, _, err = getKeys(tmReq.To, "secp256k1", "")
		if err != nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
	}

	if tmReq.ToType == "seed" {
		var err error
		_, recipientLedger, _, err = getKeys("", "BIP39", tmReq.To)
		if err != nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
	}

	if tmReq.ToType == "speed" {
		var err error
		recipientLedger, err = base64.StdEncoding.DecodeString(tmReq.To)
		if err != nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
	}

	if recipientLedger == nil {
		w.WriteHeader(http.StatusTeapot)
		return
	}

	payerBalanceBefore, err := getInAppBalance(&payerIdentity)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		tmRes.Error = err.Error()
		json.NewEncoder(w).Encode(tmRes)
		return
	}
	tmRes.Before = payerBalanceBefore

	if payerBalanceBefore < 20 {
		tmRes.After = payerBalanceBefore
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tmRes)
		return
	}

	taxSum := int64(0)
	paySum := payerBalanceBefore - taxSum

	balanceAfterTransfer, err := tmPay(payerKey, payerLedger, recipientLedger, paySum)
	if err != nil {
		tmRes.After = payerBalanceBefore
		w.WriteHeader(http.StatusInternalServerError)
		tmRes.Error = err.Error()
		json.NewEncoder(w).Encode(tmRes)
		return
	}
	tmRes.Transfer = paySum

	balanceAfterTax, err := tmPay(payerKey, payerLedger, taxLedger, taxSum)
	if err != nil {
		tmRes.After = balanceAfterTransfer
		w.WriteHeader(http.StatusInternalServerError)
		tmRes.Error = err.Error()
		json.NewEncoder(w).Encode(tmRes)
		return
	}
	tmRes.Tax = taxSum
	tmRes.After = balanceAfterTax

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tmRes)
	return
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

	taxSum := int64(0)
	paySum := payerBalanceBefore - taxSum
	if paySum <= 0 {
		io.WriteString(w, "Low amount, min: 2")
		return
	}

	_, err = pay(payerKey, payerLedger, recipientLedger, paySum)
	if err != nil {
		fmt.Println(requestId, "Error transfer:", err)
		io.WriteString(w, "Error transfer: "+err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

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

func tmPay(payerKey *ecdsa.PrivateKey, payerLedger []byte, recipientLedger []byte, amount int64) (balance int64, err error) {
	transferRequest := &ledgerpb.TransferRequest{
		Payer:     &ledgerpb.PublicKey{Key: payerLedger},
		Recipient: &ledgerpb.PublicKey{Key: recipientLedger},
		Amount:    amount,
	}

	raw, err := proto.Marshal(transferRequest)
	if err != nil {
		return 0, err
	}

	signature, err := wallet.SignChannel(raw, payerKey)
	if err != nil {
		return 0, err
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
			balance = response.Balance
			return nil
		})
	if err != nil {
		return 0, err
	}

	return balance, nil
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
	io.WriteString(w, "	toSpeed - адрес кошелька speed/btfs, пример: 047649de86edf486162563bcaf5c10c21a661a93078e0aeed5085944dab9d28df42b416e0c5dc3680788f1673d7c28648cbc856ed1fc6a375e2e3662570107deb5\n")
	io.WriteString(w, "	toKey - секретный ключ, пример: 7eb6948762712c08a1ff079dcdf8948e7e9fc9844ca9f619e770ed1fdd83ecf2\n")
	io.WriteString(w, "	toSeed - 12 слов, пример: muffin,elbow,monster,regular,burger,lady,thrive,virtual,curve,mammal,reflect,venue\n\n")

	io.WriteString(w, "Сумма списания, если сумма не указана то спишет весь баланс:\n")
	io.WriteString(w, "	amount - сумма перевода, минимальное знаение 2, 1 БТТ = 1000000\n\n")

	io.WriteString(w, "В итоге у вас должна выйти примерно такая строка, в зависимости от выбора способа указания кошелька:\n")
	io.WriteString(w, "http://193.56.8.8:30080/transfer?fromSeed=muffin,elbow,monster,regular,burger,lady,thrive,virtual,curve,mammal,reflect,venue&toSpeed=047649de86edf486162563bcaf5c10c21a661a93078e0aeed5085944dab9d28df42b416e0c5dc3680788f1673d7c28648cbc856ed1fc6a375e2e3662570107deb5&amount=2\n")
}
