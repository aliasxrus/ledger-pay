package main

import (
	"fmt"
	"github.com/gorilla/mux"
	"io"
	"net/http"
	"os"
)

// TODO 5%
func main() {
	r := mux.NewRouter()
	r.HandleFunc("/transfer", transfer)
	r.HandleFunc("/balance", balance)

	http.Handle("/", r)
	err := http.ListenAndServe(":30080", nil)
	if err != nil {
		fmt.Printf("Error ListenAndServe: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("Port: 30080")
}

func transfer(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("toKey") != "" {

	}

	// Инициализируем оба кошелька

	// Делаем перевод

	// Составляем ответ. Балансы до, балансы после, сумма перевода и комиссии, адрес кошелька трон и спида

	w.Header().Set("Content-Type", "text/plain; charset=utf-8") // normal header

	fmt.Println(q)
	w.WriteHeader(http.StatusOK)

	io.WriteString(w, "This HTTP response has both headers before this text and trailers at the end.\n")
	io.WriteString(w, "This HTTP response has both headers before this text and trailers at the end.\n")
	io.WriteString(w, "This HTTP response has both headers before this text and trailers at the end.\n")
}

func balance(w http.ResponseWriter, r *http.Request) {
	//vars := mux.Vars(r)
	//status := "AWAITING"
	//id, _ := strconv.Atoi(vars["id"])

	w.WriteHeader(http.StatusOK)
}
