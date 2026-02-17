package types

import "k8s.io/apimachinery/pkg/runtime/schema"

var TrainingPortalResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "trainingportals"}
var WorkshopResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshops"}
var WorkshopsessionsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopsessions"}
var WorkshoprequestsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshoprequests"}
var WorkshopenvironmentsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopenvironments"}
var WorkshopallocationsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopallocations"}
var SecretcopierResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretcopiers"}
var SecretinjectorsResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretinjectors"}
var SecretexportersResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretexporters"}
var SecretimportersResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretimporters"}
