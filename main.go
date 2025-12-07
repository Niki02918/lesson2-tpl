package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

type ValidationError struct {
	Line int
	Text string
}

func newRequired(field string) ValidationError {
	return ValidationError{Text: fmt.Sprintf("%s is required", field)}
}

func newType(field, typ string, line int) ValidationError {
	return ValidationError{
		Line: line,
		Text: fmt.Sprintf("%s must be %s", field, typ),
	}
}

func newInvalidFormat(field, value string, line int) ValidationError {
	return ValidationError{
		Line: line,
		Text: fmt.Sprintf("%s has invalid format '%s'", field, value),
	}
}

func newUnsupported(field, value string, line int) ValidationError {
	return ValidationError{
		Line: line,
		Text: fmt.Sprintf("%s has unsupported value '%s'", field, value),
	}
}

func newOutOfRange(field string, line int) ValidationError {
	return ValidationError{
		Line: line,
		Text: fmt.Sprintf("%s value out of range", field),
	}
}

func getMapValue(n *yaml.Node, key string) (*yaml.Node, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1], true
		}
	}
	return nil, false
}

func validatePod(doc *yaml.Node) []ValidationError {
	var errs []ValidationError

	if doc.Kind != yaml.MappingNode {
		errs = append(errs, newType("root", "object", doc.Line))
		return errs
	}

	apiVersion, ok := getMapValue(doc, "apiVersion")
	if !ok {
		errs = append(errs, newRequired("apiVersion"))
	} else if apiVersion.Kind != yaml.ScalarNode {
		errs = append(errs, newType("apiVersion", "string", apiVersion.Line))
	} else if apiVersion.Value != "v1" {
		errs = append(errs, newUnsupported("apiVersion", apiVersion.Value, apiVersion.Line))
	}

	kind, ok := getMapValue(doc, "kind")
	if !ok {
		errs = append(errs, newRequired("kind"))
	} else if kind.Kind != yaml.ScalarNode {
		errs = append(errs, newType("kind", "string", kind.Line))
	} else if kind.Value != "Pod" {
		errs = append(errs, newUnsupported("kind", kind.Value, kind.Line))
	}

	meta, ok := getMapValue(doc, "metadata")
	if !ok {
		errs = append(errs, newRequired("metadata"))
	} else {
		errs = append(errs, validateMetadata(meta)...)
	}

	spec, ok := getMapValue(doc, "spec")
	if !ok {
		errs = append(errs, newRequired("spec"))
	} else {
		errs = append(errs, validateSpec(spec)...)
	}

	return errs
}

func validateMetadata(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.MappingNode {
		errs = append(errs, newType("metadata", "object", n.Line))
		return errs
	}

	name, ok := getMapValue(n, "name")
	if !ok {
		errs = append(errs, newRequired("name"))
	} else if name.Kind != yaml.ScalarNode {
		errs = append(errs, newType("name", "string", name.Line))
	} else if name.Value == "" {
		errs = append(errs, ValidationError{Line: name.Line, Text: "name is required"})
	}

	if ns, ok := getMapValue(n, "namespace"); ok && ns.Kind != yaml.ScalarNode {
		errs = append(errs, newType("namespace", "string", ns.Line))
	}

	if labels, ok := getMapValue(n, "labels"); ok {
		if labels.Kind != yaml.MappingNode {
			errs = append(errs, newType("labels", "object", labels.Line))
		} else {
			for i := 0; i < len(labels.Content); i += 2 {
				if labels.Content[i+1].Kind != yaml.ScalarNode {
					errs = append(errs, newType("labels", "string", labels.Content[i+1].Line))
				}
			}
		}
	}

	return errs
}

func validateSpec(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.MappingNode {
		errs = append(errs, newType("spec", "object", n.Line))
		return errs
	}

	if osnode, ok := getMapValue(n, "os"); ok {
		if osnode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("os", "string", osnode.Line))
		} else if osnode.Value != "linux" && osnode.Value != "windows" {
			errs = append(errs, newUnsupported("os", osnode.Value, osnode.Line))
		}
	}

	containers, ok := getMapValue(n, "containers")
	if !ok {
		errs = append(errs, newRequired("containers"))
	} else {
		errs = append(errs, validateContainers(containers)...)
	}

	return errs
}

var imageRe = regexp.MustCompile(`^registry\.bigbrother\.io\/[^:]+:[^:]+$`)
var cnameRe = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

func validateContainers(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.SequenceNode {
		errs = append(errs, newType("containers", "array", n.Line))
		return errs
	}

	seen := map[string]bool{}

	for _, c := range n.Content {
		if c.Kind != yaml.MappingNode {
			errs = append(errs, newType("containers", "object", c.Line))
			continue
		}

		name, ok := getMapValue(c, "name")
		if !ok {
			errs = append(errs, newRequired("name"))
		} else if name.Kind != yaml.ScalarNode {
			errs = append(errs, newType("name", "string", name.Line))
		} else if name.Value == "" {
			errs = append(errs, ValidationError{name.Line, "name is required"})
		} else {
			if !cnameRe.MatchString(name.Value) {
				errs = append(errs, newInvalidFormat("name", name.Value, name.Line))
			}
			if seen[name.Value] {
				errs = append(errs, newInvalidFormat("name", name.Value, name.Line))
			}
			seen[name.Value] = true
		}

		image, ok := getMapValue(c, "image")
		if !ok {
			errs = append(errs, newRequired("image"))
		} else if image.Kind != yaml.ScalarNode {
			errs = append(errs, newType("image", "string", image.Line))
		} else if !imageRe.MatchString(image.Value) {
			errs = append(errs, newInvalidFormat("image", image.Value, image.Line))
		}

		if ports, ok := getMapValue(c, "ports"); ok {
			errs = append(errs, validatePorts(ports)...)
		}

		if rp, ok := getMapValue(c, "readinessProbe"); ok {
			errs = append(errs, validateProbe(rp)...)
		}

		if lp, ok := getMapValue(c, "livenessProbe"); ok {
			errs = append(errs, validateProbe(lp)...)
		}

		res, ok := getMapValue(c, "resources")
		if !ok {
			errs = append(errs, newRequired("resources"))
		} else {
			errs = append(errs, validateResources(res)...)
		}
	}

	return errs
}

func validatePorts(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.SequenceNode {
		errs = append(errs, newType("ports", "array", n.Line))
		return errs
	}

	for _, p := range n.Content {
		if p.Kind != yaml.MappingNode {
			errs = append(errs, newType("ports", "object", p.Line))
			continue
		}

		cp, ok := getMapValue(p, "containerPort")
		if !ok {
			errs = append(errs, newRequired("containerPort"))
		} else if cp.Kind != yaml.ScalarNode {
			errs = append(errs, newType("containerPort", "int", cp.Line))
		} else {
			v, err := strconv.Atoi(cp.Value)
			if err != nil {
				errs = append(errs, newType("containerPort", "int", cp.Line))
			} else if v <= 0 || v >= 65536 {
				errs = append(errs, newOutOfRange("containerPort", cp.Line))
			}
		}

		if proto, ok := getMapValue(p, "protocol"); ok {
			if proto.Kind != yaml.ScalarNode {
				errs = append(errs, newType("protocol", "string", proto.Line))
			} else if proto.Value != "TCP" && proto.Value != "UDP" {
				errs = append(errs, newUnsupported("protocol", proto.Value, proto.Line))
			}
		}
	}

	return errs
}

func validateProbe(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.MappingNode {
		errs = append(errs, newType("probe", "object", n.Line))
		return errs
	}

	hg, ok := getMapValue(n, "httpGet")
	if !ok {
		errs = append(errs, newRequired("httpGet"))
		return errs
	}

	if hg.Kind != yaml.MappingNode {
		errs = append(errs, newType("httpGet", "object", hg.Line))
		return errs
	}

	path, ok := getMapValue(hg, "path")
	if !ok {
		errs = append(errs, newRequired("path"))
	} else if path.Kind != yaml.ScalarNode {
		errs = append(errs, newType("path", "string", path.Line))
	} else if len(path.Value) == 0 || path.Value[0] != '/' {
		errs = append(errs, newInvalidFormat("path", path.Value, path.Line))
	}

	port, ok := getMapValue(hg, "port")
	if !ok {
		errs = append(errs, newRequired("port"))
	} else if port.Kind != yaml.ScalarNode {
		errs = append(errs, newType("port", "int", port.Line))
	} else {
		v, err := strconv.Atoi(port.Value)
		if err != nil {
			errs = append(errs, newType("port", "int", port.Line))
		} else if v <= 0 || v >= 65536 {
			errs = append(errs, newOutOfRange("port", port.Line))
		}
	}

	return errs
}

var memRe = regexp.MustCompile(`^[0-9]+(Mi|Gi|Ki)$`)

func validateResources(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.MappingNode {
		errs = append(errs, newType("resources", "object", n.Line))
		return errs
	}

	if limits, ok := getMapValue(n, "limits"); ok {
		errs = append(errs, validateResourceMap(limits)...)
	}

	if req, ok := getMapValue(n, "requests"); ok {
		errs = append(errs, validateResourceMap(req)...)
	}

	return errs
}

func validateResourceMap(n *yaml.Node) []ValidationError {
	var errs []ValidationError

	if n.Kind != yaml.MappingNode {
		errs = append(errs, newType("resources", "object", n.Line))
		return errs
	}

	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		v := n.Content[i+1]

		switch k.Value {
		case "cpu":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!int" {
				errs = append(errs, newType("cpu", "int", v.Line))
			}
		case "memory":
			if v.Kind != yaml.ScalarNode {
				errs = append(errs, newType("memory", "string", v.Line))
			} else if !memRe.MatchString(v.Value) {
				errs = append(errs, newInvalidFormat("memory", v.Value, v.Line))
			}
		}
	}

	return errs
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stdout, "usage: yamlvalid <path>")
		os.Exit(1)
	}

	full := os.Args[1]
	base := filepath.Base(full)

	data, err := os.ReadFile(full)
	if err != nil {
		fmt.Fprintf(os.Stdout, "cannot read file '%s': %v\n", full, err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stdout, "%s: cannot unmarshal yaml: %v\n", base, err)
		os.Exit(1)
	}

	if len(root.Content) == 0 {
		fmt.Fprintf(os.Stdout, "%s: empty yaml document\n", base)
		os.Exit(1)
	}

	errs := validatePod(root.Content[0])
	if len(errs) > 0 {
		for _, e := range errs {
			if e.Line > 0 {
				fmt.Fprintf(os.Stdout, "%s:%d %s\n", base, e.Line, e.Text)
			} else {
				fmt.Fprintf(os.Stdout, "%s: %s\n", base, e.Text)
			}
		}
		os.Exit(1)
	}

	os.Exit(0)
}
