package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	vault "github.com/hashicorp/vault/api"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/klog"
)

type server struct {
	vault *vault.Client
}

// Based on https://medium.com/ovni/writing-a-very-basic-kubernetes-mutating-admission-webhook-398dbbcb63ec

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!")
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "ok")
}

func (s *server) handleMutate(w http.ResponseWriter, r *http.Request) {
	// Decode the request
	body, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s", err)
		return
	}

	admissionReview := v1beta1.AdmissionReview{}
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s", err)
		return
	}

	response, err := s.mutate(*admissionReview.Request)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s", err)
		return
	}

	reviewResponse := v1beta1.AdmissionReview{
		Response: &response,
	}

	if body, err = json.Marshal(reviewResponse); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "%s", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func main() {
	var err error
	s := server{}

	s.vault, err = vault.NewClient(&vault.Config{
		AgentAddress: os.Getenv("VAULT_AGENT_ADDR"),
	})
	if err != nil {
		klog.Fatalf("Error initializing Vault client: %s", err)
	}

	// secret, err := vc.Logical().Read("minio_minimal_tenant1/keys/profile-will-hearn")
	// if err != nil {
	// 	klog.Fatalf("error fetching minio credentials: %v", err)
	// }

	// klog.Infof("%v", secret)

	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/_healthz", s.handleHealthz)
	mux.HandleFunc("/mutate", s.handleMutate)

	httpServer := &http.Server{
		Addr:           ":8443",
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	log.Println("Listening on :8443")
	log.Fatal(httpServer.ListenAndServeTLS("./certs/tls.crt", "./certs/tls.key"))
}
