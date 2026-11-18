package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClientCredentials holds the secret a client logs in with.
type ClientCredentials struct {
	// password is what the client sends to /auth/login with its name.
	Password string `json:"password"`
}

// ClientConfigSpec describes an API client of the Lookup Service.
type ClientConfigSpec struct {
	// client holds the client's credentials.
	Client ClientCredentials `json:"client"`

	// user, when set, is the user every session request of this client is
	// made for, whatever the request's clientUserId says.
	// +optional
	User string `json:"user,omitempty"`

	// roles are the roles the client holds, such as admin and tenant.
	Roles []string `json:"roles"`

	// tenants are glob patterns naming the tenants the client may act for.
	// +optional
	Tenants []string `json:"tenants,omitempty"`
}

// ClientConfig is an API client of the Lookup Service. Its name is the name
// the client logs in with.
//
// +kubebuilder:object:root=true
type ClientConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClientConfigSpec `json:"spec"`
}

// ClientConfigList is a list of ClientConfigs.
//
// +kubebuilder:object:root=true
type ClientConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClientConfig `json:"items"`
}

func init() {
	register(&ClientConfig{}, &ClientConfigList{})
}
