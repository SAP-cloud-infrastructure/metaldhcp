// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	metaldhcpv1alpha1 "github.com/SAP-cloud-infrastructure/metaldhcp/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	review := &admissionv1.AdmissionReview{}
	if err := json.Unmarshal(body, review); err != nil {
		http.Error(w, "failed to decode AdmissionReview", http.StatusBadRequest)
		return
	}
	if review.Request == nil {
		http.Error(w, "AdmissionReview request is nil", http.StatusBadRequest)
		return
	}

	subnet := &metaldhcpv1alpha1.OOBSubnet{}
	if err := json.Unmarshal(review.Request.Object.Raw, subnet); err != nil {
		writeResponse(w, review.Request.UID, false, fmt.Sprintf("failed to decode OOBSubnet: %v", err))
		return
	}

	var valErr error
	switch review.Request.Operation {
	case admissionv1.Create:
		_, valErr = subnet.ValidateCreate()
	case admissionv1.Update:
		_, valErr = subnet.ValidateUpdate(nil)
	case admissionv1.Delete:
		_, valErr = subnet.ValidateDelete()
	}

	if valErr != nil {
		writeResponse(w, review.Request.UID, false, valErr.Error())
		return
	}
	writeResponse(w, review.Request.UID, true, "")
}

func writeResponse(w http.ResponseWriter, uid types.UID, allowed bool, msg string) {
	resp := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     uid,
			Allowed: allowed,
		},
	}
	if msg != "" {
		resp.Response.Result = &metav1.Status{Message: msg}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
