package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

func cleanName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

func (s *server) mutate(request v1beta1.AdmissionRequest) (v1beta1.AdmissionResponse, error) {
	response := v1beta1.AdmissionResponse{}

	// Default response
	response.Allowed = true
	response.UID = request.UID

	patch := v1beta1.PatchTypeJSONPatch
	response.PatchType = &patch

	patches := make([]map[string]interface{}, 0)

	// Decode the pod object
	var err error
	pod := v1.Pod{}
	if err = json.Unmarshal(request.Object.Raw, &pod); err != nil {
		return response, fmt.Errorf("unable to decode Pod %w", err)
	}

	log.Printf("Check pod for notebook %s/%s", pod.Namespace, pod.Name)

	// If we have a notebook, then lets run the logic
	if _, ok := pod.ObjectMeta.Labels["notebook-name"]; ok {
		// Attempt to request a token from Vault for Minio
		creds, err := s.vault.Logical().Read(fmt.Sprintf("minio_minimal_tenant1/keys/profile-%s", cleanName(pod.Namespace)))
		if err != nil {
			klog.Warningf("unable to obtain MinIO token: %v", err)
			return response, nil
		}

		patches = append(patches, map[string]interface{}{
			"op":   "add",
			"path": "/spec/volumes/-",
			"value": v1.Volume{
				Name: "minio-minimal-tenant1-private",
				VolumeSource: v1.VolumeSource{
					FlexVolume: &v1.FlexVolumeSource{
						Driver: "informaticslab/goofys-flex-volume",
						Options: map[string]string{
							"bucket":     cleanName(pod.Namespace),
							"endpoint":   "https://minimal-tenant1-minio.covid.cloud.statcan.ca",
							"region":     "us-east-1",
							"access-key": creds.Data["accessKeyId"].(string),
							"secret-key": creds.Data["secretAccessKey"].(string),
						},
					},
				},
			},
		})

		patches = append(patches, map[string]interface{}{
			"op":   "add",
			"path": "/spec/containers/0/volumeMounts/-",
			"value": v1.VolumeMount{
				Name:      "minio-minimal-tenant1-private",
				MountPath: "/minio/minimal-tenant1/private",
			},
		})

		response.AuditAnnotations = map[string]string{
			"goofys-injector": "Added MinIO volume mounts",
		}
		response.Patch, err = json.Marshal(patches)
		if err != nil {
			return response, err
		}

		response.Result = &metav1.Status{
			Status: metav1.StatusSuccess,
		}
	}

	return response, nil
}
