package data

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Dataset defines a test dataset.
type Dataset struct {
	Name       string            `json:"name" yaml:"name"`
	File       string            `json:"file" yaml:"file"`
	Bucket     string            `json:"bucket,omitempty" yaml:"bucket,omitempty"`
	Source     string            `json:"source,omitempty" yaml:"source,omitempty"`
	Index      string            `json:"index" yaml:"index"`
	Sourcetype string            `json:"sourcetype" yaml:"sourcetype"`
	Count      int               `json:"count" yaml:"count"`
	Settings   map[string]string `json:"settings,omitempty" yaml:"settings,omitempty"`
}

// Registry holds datasets keyed by name.
type Registry struct {
	Datasets map[string]Dataset `json:"datasets" yaml:"datasets"`
}

// LoadRegistry reads a registry YAML file.
func LoadRegistry(path string) (*Registry, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg Registry
	if err := yaml.Unmarshal(payload, &reg); err != nil {
		return nil, err
	}
	if reg.Datasets == nil {
		reg.Datasets = make(map[string]Dataset)
	}
	for key, dataset := range reg.Datasets {
		reg.Datasets[key] = expandDataset(dataset)
	}
	return &reg, nil
}

func expandDataset(dataset Dataset) Dataset {
	dataset.Name = os.ExpandEnv(dataset.Name)
	dataset.File = os.ExpandEnv(dataset.File)
	dataset.Bucket = os.ExpandEnv(dataset.Bucket)
	dataset.Source = os.ExpandEnv(dataset.Source)
	dataset.Index = os.ExpandEnv(dataset.Index)
	dataset.Sourcetype = os.ExpandEnv(dataset.Sourcetype)
	if dataset.Settings != nil {
		for key, value := range dataset.Settings {
			dataset.Settings[key] = os.ExpandEnv(value)
		}
	}
	return dataset
}

// Get returns a dataset by name.
func (r *Registry) Get(name string) (Dataset, bool) {
	ds, ok := r.Datasets[name]
	return ds, ok
}
