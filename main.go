package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/api/admission/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var apiserver string
var kubeconfig string
var rootCmd = &cobra.Command{
	Use:   "blob-csi-webhook",
	Short: "A Mutating webhook for automounting labeled PVCs",
	Long:  `A Mutating webhook for automounting labeled PVCs`,
}

type server struct {
	client *kubernetes.Clientset
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

	rootCmd.PersistentFlags().StringVar(&apiserver, "apiserver", "", "URL to the Kubernetes API server")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to the Kubeconfig file")
	rootCmd.Execute()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags(apiserver, kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	s := server{clientset}

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
