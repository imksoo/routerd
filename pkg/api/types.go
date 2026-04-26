package api

type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

type ObjectMeta struct {
	Name string `yaml:"name" json:"name"`
}

type Router struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta `yaml:"metadata" json:"metadata"`
	Spec     RouterSpec `yaml:"spec" json:"spec"`
}

type RouterSpec struct {
	Resources []Resource `yaml:"resources" json:"resources"`
}

type Resource struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta     `yaml:"metadata" json:"metadata"`
	Spec     map[string]any `yaml:"spec" json:"spec"`
	Status   map[string]any `yaml:"status,omitempty" json:"status,omitempty"`
}

func (r Resource) ID() string {
	return r.APIVersion + "/" + r.Kind + "/" + r.Metadata.Name
}

const (
	RouterAPIVersion = "routerd.net/v1alpha1"
	NetAPIVersion    = "net.routerd.net/v1alpha1"
)
