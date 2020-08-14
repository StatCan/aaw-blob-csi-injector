package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"

	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

const defaultRegion = "us-east-1"

func cleanName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

func (s *server) addMount(name, accessKey, secretKey, endpoint, region, bucket, mountPath string) []map[string]interface{} {
	patches := make([]map[string]interface{}, 0)

	// Add volume definition
	patches = append(patches, map[string]interface{}{
		"op":   "add",
		"path": "/spec/volumes/-",
		"value": v1.Volume{
			Name: name,
			VolumeSource: v1.VolumeSource{
				FlexVolume: &v1.FlexVolumeSource{
					Driver: "informaticslab/goofys-flex-volume",
					Options: map[string]string{
						"bucket":     bucket,
						"endpoint":   endpoint,
						"region":     region,
						"access-key": accessKey,
						"secret-key": secretKey,
						"uid":        "1000",
						"gid":        "100",
					},
				},
			},
		},
	})

	// Add VolumeMount
	patches = append(patches, map[string]interface{}{
		"op":   "add",
		"path": "/spec/containers/0/volumeMounts/-",
		"value": v1.VolumeMount{
			Name:      name,
			MountPath: mountPath,
		},
	})

	return patches
}

func (s *server) addInstance(instance, mount, endpoint, region, profile, base string) []map[string]interface{} {
	patches := make([]map[string]interface{}, 0)

	// Attempt to request a token from Vault for Minio
	creds, err := s.vault.Logical().Read(fmt.Sprintf("%s/keys/profile-%s", mount, profile))
	if err != nil {
		klog.Warningf("unable to obtain MinIO token at %s/%s: %v", mount, profile, err)
		return patches
	}

	accessKey := creds.Data["accessKeyId"].(string)
	secretKey := creds.Data["secretAccessKey"].(string)

	// Mount private
	patches = append(patches, s.addMount(fmt.Sprintf("%s-private", instance), accessKey, secretKey, endpoint, region, profile, path.Join(base, "private"))...)

	// Mount shared
	patches = append(patches, s.addMount(fmt.Sprintf("%s-shared", instance), accessKey, secretKey, endpoint, region, "shared", path.Join(base, "shared"))...)

	return patches
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

	profile := cleanName(pod.Namespace)

	// If we have a notebook, then lets run the logic
	if _, ok := pod.ObjectMeta.Labels["notebook-name"]; ok && (profile == "zachary-seguin" || profile == "will-hearn" || profile == "blair-drummond") {
		patches = append(patches, s.addInstance("minimal-minio-tenant1", "minio_minimal_tenant1", "https://minimal-tenant1-minio.covid.cloud.statcan.ca", defaultRegion, profile, "/home/jovyan/minio/minimal-tenant1")...)
		patches = append(patches, s.addInstance("premium-minio-tenant1", "minio_premium_tenant1", "https://premium-tenant1-minio.covid.cloud.statcan.ca", defaultRegion, profile, "/home/jovyan/minio/premium-tenant1")...)

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
