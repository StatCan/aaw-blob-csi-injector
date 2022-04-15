package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const injectionLabel = "data.statcan.gc.ca/inject-blob-volumes"
const automountLabel = "blob.aaw.statcan.gc.ca/automount"
const classificationLabel = "data.statcan.gc.ca/classification"

// TODO: make sure the PV & PVCs are not in terminating state.
func (s *server) getBinds(pod v1.Pod) ([]v1.PersistentVolumeClaim, error) {

	// classificationLabel: "protected-b",
	var selector metav1.LabelSelector
	if classification, ok := pod.ObjectMeta.Labels[classificationLabel]; ok && classification == "protected-b" {
		// automount && protected-b == true
		selector = metav1.LabelSelector{
			MatchLabels: map[string]string{
				classificationLabel: classification,
				automountLabel:      "true",
			},
		}
	} else {
		// automount && !(protected-b == true)
		selector = metav1.LabelSelector{
			MatchLabels: map[string]string{
				automountLabel: "true",
			},
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      classificationLabel,
					Operator: metav1.LabelSelectorOpNotIn,
					Values: []string{
						"protected-b",
					},
				},
			},
		}
	}

	//
	selectorStr, _ := metav1.LabelSelectorAsSelector(&selector)

	pvcs, _ := s.client.CoreV1().PersistentVolumeClaims(pod.Namespace).List(
		context.Background(),
		metav1.ListOptions{},
	)

	pvcs, err := s.client.CoreV1().PersistentVolumeClaims(pod.Namespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: selectorStr.String(),
		},
	)
	if err != nil {
		return []v1.PersistentVolumeClaim{}, err
	}

	return pvcs.Items, nil
}

func (s *server) addVolumeMount(name, mountPath string, readOnly bool, containerIndex int) []map[string]interface{} {
	patches := make([]map[string]interface{}, 0)

	// Add volume definition
	patches = append(patches, map[string]interface{}{
		"op":   "add",
		"path": "/spec/volumes/-",
		"value": v1.Volume{
			Name: name,
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					ClaimName: name,
					// ReadOnly:  readOnly, // I think the underlying PV handles this. TODO: Check.
				},
			},
		},
	})

	// Add VolumeMount
	patches = append(patches, map[string]interface{}{
		"op":   "add",
		"path": fmt.Sprintf("/spec/containers/%d/volumeMounts/-", containerIndex),
		"value": v1.VolumeMount{
			Name:      name,
			MountPath: mountPath,
		},
	})

	return patches
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}

func (s *server) mutate(request v1beta1.AdmissionRequest) (v1beta1.AdmissionResponse, error) {
	response := v1beta1.AdmissionResponse{}

	log.Println(prettyPrint(request))

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

	// We have to populate this from the
	// AdmissionReview object?
	pod.Name = request.Name
	pod.Namespace = request.Namespace

	log.Printf(
		"Check pod for notebook %s/%s",
		pod.Name,
		pod.Namespace,
	)

	// Only inject when matching label
	inject := false
	containerIndex := 0

	// If we have the right annotations
	if val, ok := pod.ObjectMeta.Annotations[injectionLabel]; ok {
		bval, err := strconv.ParseBool(val)
		if err != nil {
			log.Printf("Failed to parse injection label for %s/%s", pod.Name, pod.Namespace)
			return response, fmt.Errorf("unable to decode %s annotation %w", injectionLabel, err)
		}
		inject = bval
	}

	// If we have a Argo workflow, then lets run the logic
	if _, ok := pod.ObjectMeta.Labels["workflows.argoproj.io/workflow"]; ok {
		// Check the name of the first container in the pod.
		// If it's called "wait", then we want to add the mount to the second container.
		if pod.Spec.Containers[0].Name == "wait" {
			containerIndex = 1
		} else {
			containerIndex = 0
		}
	}

	log.Println(pod)

	if inject {
		log.Printf("Injecting pod %s/%s ...\n", pod.Name, pod.Namespace)
		pvcs, err := s.getBinds(pod)

		// Add all PVC patches
		for _, pvc := range pvcs {
			log.Println(fmt.Sprintf("/home/jovyan/buckets/%s", pvc.Name))
			pvcpatches := s.addVolumeMount(
				pvc.Name,
				fmt.Sprintf("/home/jovyan/buckets/%s", pvc.Name),
				false,
				containerIndex,
			)
			patches = append(patches, pvcpatches...)
		}

		response.AuditAnnotations = map[string]string{
			"blob-csi-injector": "Added Blob-CSI volume mounts",
		}
		response.Patch, err = json.Marshal(patches)
		if err != nil {
			return response, err
		}

		response.Result = &metav1.Status{
			Status: metav1.StatusSuccess,
		}
	} else {
		log.Printf("Skipping pod %s/%s.\n", pod.Name, pod.Namespace)
	}

	return response, nil
}
