package spec

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadSpecs reads all spec files from a directory recursively.
func LoadSpecs(root string) ([]TestSpec, error) {
	var specs []TestSpec

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isSpecFile(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var spec TestSpec
			if err := decoder.Decode(&spec); err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			if spec.Metadata.Name == "" && spec.Kind == "" && spec.APIVersion == "" && len(spec.Steps) == 0 {
				continue
			}
			if spec.Metadata.Name == "" {
				spec.Metadata.Name = filepath.Base(path)
			}
			if len(spec.Variants) > 0 {
				specs = append(specs, expandVariants(spec)...)
			} else {
				specs = append(specs, spec)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return specs, nil
}

func isSpecFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func expandVariants(base TestSpec) []TestSpec {
	out := make([]TestSpec, 0, len(base.Variants))
	for i, variant := range base.Variants {
		specCopy := base
		specCopy.Variants = nil
		specCopy.Metadata = base.Metadata
		specCopy.Metadata.Name = variantName(base.Metadata.Name, variant, i)
		specCopy.Metadata.Tags = mergeTags(base.Metadata.Tags, variant.Tags)
		specCopy.Topology.Params = copyStringMap(base.Topology.Params)
		specCopy.Steps = copySteps(base.Steps)
		if len(variant.Params) > 0 {
			if specCopy.Topology.Params == nil {
				specCopy.Topology.Params = make(map[string]string, len(variant.Params))
			}
			for key, value := range variant.Params {
				if strings.TrimSpace(value) == "" {
					continue
				}
				specCopy.Topology.Params[key] = value
			}
		}
		if len(variant.StepOverrides) > 0 {
			specCopy.Steps = applyStepOverrides(specCopy.Steps, variant.StepOverrides)
		}
		out = append(out, specCopy)
	}
	return out
}

func variantName(baseName string, variant VariantSpec, index int) string {
	if name := strings.TrimSpace(variant.Name); name != "" {
		return name
	}
	if suffix := strings.TrimSpace(variant.NameSuffix); suffix != "" {
		return fmt.Sprintf("%s-%s", baseName, suffix)
	}
	if index >= 0 {
		return fmt.Sprintf("%s-%d", baseName, index+1)
	}
	return baseName
}

func mergeTags(base []string, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	add := func(tag string) {
		value := strings.TrimSpace(tag)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, value)
	}
	for _, tag := range base {
		add(tag)
	}
	for _, tag := range extra {
		add(tag)
	}
	return out
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func copySteps(steps []StepSpec) []StepSpec {
	if len(steps) == 0 {
		return nil
	}
	out := make([]StepSpec, len(steps))
	for i, step := range steps {
		out[i] = step
		out[i].With = copyWithMap(step.With)
	}
	return out
}

func copyWithMap(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func applyStepOverrides(steps []StepSpec, overrides []StepOverride) []StepSpec {
	if len(overrides) == 0 {
		return steps
	}
	out := copySteps(steps)
	for _, override := range overrides {
		name := strings.TrimSpace(override.Name)
		if name == "" {
			continue
		}
		index := -1
		for i := range out {
			if strings.EqualFold(out[i].Name, name) {
				index = i
				break
			}
		}
		if index == -1 {
			out = append(out, StepSpec{
				Name:   name,
				Action: override.Action,
				With:   copyWithMap(override.With),
			})
			continue
		}
		if override.Replace {
			out[index] = StepSpec{
				Name:   name,
				Action: override.Action,
				With:   copyWithMap(override.With),
			}
			continue
		}
		if override.Action != "" {
			out[index].Action = override.Action
		}
		if override.With != nil {
			if out[index].With == nil {
				out[index].With = make(map[string]interface{}, len(override.With))
			}
			for key, value := range override.With {
				if value == nil {
					delete(out[index].With, key)
					continue
				}
				out[index].With[key] = value
			}
		}
	}
	return out
}
