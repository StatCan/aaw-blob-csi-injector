# Blob CSI Injector

A mutating webhook which injects `volumes` and `volumeMounts` into pods with the `data.statcan.gc.ca/inject-blob-volumes` label.

- Use the `blob.aaw.statcan.gc.ca/automount` label to select PVCs to inject.
- Differentiate between `protected-b` and `unclassified` PVCs/notebooks, and only inject if the classifications match between the Pod and PVC
- These specific PVCs are created by a profile controller which statically provisions the PVs for backing buckets.

*Technically nothing here is specific to blob-csi PVCs, it was simply designed with this purpose in mind.*


## References

- [Blob CSI Architecture](https://github.com/StatCan/daaas/issues/1001)
- [Mutating WebHook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#mutatingadmissionwebhook)
- [The blob-csi profile controller](https://github.com/StatCan/aaw-kubeflow-profiles-controller/blob/main/cmd/blob-csi.go)
