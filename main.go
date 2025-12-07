package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

type ValidationError struct {
	Line int    // 0 — если строка неизвестна
	Text string // готовое сообщение без имени файла
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

// getMapValue возвращает значение по ключу из MappingNode.
func getMapValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		if k.Value == key {
			return v, true
		}
	}
	return nil, false
}

// ---------- Валидация верхнего уровня (Pod) ----------

func validatePod(doc *yaml.Node) []ValidationError {
	var errs []ValidationError

	if doc.Kind != yaml.MappingNode {
		errs = append(errs, newType("root", "object", doc.Line))
		return errs
	}

	// apiVersion: required, string, = "v1"
	apiVersionNode, ok := getMapValue(doc, "apiVersion")
	if !ok {
		errs = append(errs, newRequired("apiVersion"))
	} else {
		if apiVersionNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("apiVersion", "string", apiVersionNode.Line))
		} else if apiVersionNode.Value != "v1" {
			errs = append(errs, newUnsupported("apiVersion", apiVersionNode.Value, apiVersionNode.Line))
		}
	}

	// kind: required, string, = "Pod"
	kindNode, ok := getMapValue(doc, "kind")
	if !ok {
		errs = append(errs, newRequired("kind"))
	} else {
		if kindNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("kind", "string", kindNode.Line))
		} else if kindNode.Value != "Pod" {
			errs = append(errs, newUnsupported("kind", kindNode.Value, kindNode.Line))
		}
	}

	// metadata: required ObjectMeta
	metadataNode, ok := getMapValue(doc, "metadata")
	if !ok {
		errs = append(errs, newRequired("metadata"))
	} else {
		errs = append(errs, validateMetadata(metadataNode)...)
	}

	// spec: required PodSpec
	specNode, ok := getMapValue(doc, "spec")
	if !ok {
		errs = append(errs, newRequired("spec"))
	} else {
		errs = append(errs, validateSpec(specNode)...)
	}

	return errs
}

// ---------- ObjectMeta ----------

func validateMetadata(node *yaml.Node) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.MappingNode {
		errs = append(errs, newType("metadata", "object", node.Line))
		return errs
	}

	// name: required string
	nameNode, ok := getMapValue(node, "name")
	if !ok {
		errs = append(errs, newRequired("metadata.name"))
	} else {
		if nameNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("metadata.name", "string", nameNode.Line))
		}
	}

	// namespace: optional string
	if nsNode, ok := getMapValue(node, "namespace"); ok {
		if nsNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("metadata.namespace", "string", nsNode.Line))
		}
	}

	// labels: optional object<string,string>
	if labelsNode, ok := getMapValue(node, "labels"); ok {
		if labelsNode.Kind != yaml.MappingNode {
			errs = append(errs, newType("metadata.labels", "object", labelsNode.Line))
		} else {
			for i := 0; i < len(labelsNode.Content); i += 2 {
				v := labelsNode.Content[i+1]
				if v.Kind != yaml.ScalarNode {
					errs = append(errs, newType("metadata.labels", "string", v.Line))
				}
			}
		}
	}

	return errs
}

// ---------- PodSpec ----------

func validateSpec(node *yaml.Node) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.MappingNode {
		errs = append(errs, newType("spec", "object", node.Line))
		return errs
	}

	// os: optional, string: linux | windows
	if osNode, ok := getMapValue(node, "os"); ok {
		if osNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("os", "string", osNode.Line))
		} else {
			switch osNode.Value {
			case "linux", "windows":
				// ok
			default:
				errs = append(errs, newUnsupported("os", osNode.Value, osNode.Line))
			}
		}
	}

	// containers: required, list of Container
	containersNode, ok := getMapValue(node, "containers")
	if !ok {
		errs = append(errs, newRequired("spec.containers"))
	} else {
		errs = append(errs, validateContainers(containersNode)...)
	}

	return errs
}

// ---------- Containers ----------

var containerNameRe = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
var imageRe = regexp.MustCompile(`^registry\.bigbrother\.io\/[^:]+:[^:]+$`)

func validateContainers(node *yaml.Node) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.SequenceNode {
		errs = append(errs, newType("containers", "array", node.Line))
		return errs
	}

	seenNames := make(map[string]struct{})

	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			errs = append(errs, newType("container", "object", item.Line))
			continue
		}

		// name
		nameNode, ok := getMapValue(item, "name")
		if !ok {
			errs = append(errs, newRequired("containers.name"))
		} else {
			if nameNode.Kind != yaml.ScalarNode {
				errs = append(errs, newType("containers.name", "string", nameNode.Line))
			} else {
				name := nameNode.Value
				if !containerNameRe.MatchString(name) {
					errs = append(errs, newInvalidFormat("containers.name", name, nameNode.Line))
				}
				if _, exists := seenNames[name]; exists {
					// имя должно быть уникальным
					errs = append(errs, newInvalidFormat("containers.name", name, nameNode.Line))
				}
				seenNames[name] = struct{}{}
			}
		}

		// image
		imageNode, ok := getMapValue(item, "image")
		if !ok {
			errs = append(errs, newRequired("image"))
		} else {
			if imageNode.Kind != yaml.ScalarNode {
				errs = append(errs, newType("image", "string", imageNode.Line))
			} else if !imageRe.MatchString(imageNode.Value) {
				errs = append(errs, newInvalidFormat("image", imageNode.Value, imageNode.Line))
			}
		}

		// ports: optional
		if portsNode, ok := getMapValue(item, "ports"); ok {
			errs = append(errs, validatePorts(portsNode)...)
		}

		// readinessProbe: optional
		if rpNode, ok := getMapValue(item, "readinessProbe"); ok {
			errs = append(errs, validateProbe(rpNode, "readinessProbe")...)
		}

		// livenessProbe: optional
		if lpNode, ok := getMapValue(item, "livenessProbe"); ok {
			errs = append(errs, validateProbe(lpNode, "livenessProbe")...)
		}

		// resources: required
		resNode, ok := getMapValue(item, "resources")
		if !ok {
			errs = append(errs, newRequired("resources"))
		} else {
			errs = append(errs, validateResources(resNode)...)
		}
	}

	return errs
}

// ---------- ContainerPort ----------

func validatePorts(node *yaml.Node) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.SequenceNode {
		errs = append(errs, newType("ports", "array", node.Line))
		return errs
	}

	for _, p := range node.Content {
		if p.Kind != yaml.MappingNode {
			errs = append(errs, newType("ports", "object", p.Line))
			continue
		}

		// containerPort: required int 0<x<65536
		cpNode, ok := getMapValue(p, "containerPort")
		if !ok {
			errs = append(errs, newRequired("containerPort"))
		} else {
			if cpNode.Kind != yaml.ScalarNode {
				errs = append(errs, newType("containerPort", "int", cpNode.Line))
			} else {
				port, err := strconv.Atoi(cpNode.Value)
				if err != nil {
					errs = append(errs, newType("containerPort", "int", cpNode.Line))
				} else if port <= 0 || port >= 65536 {
					errs = append(errs, newOutOfRange("containerPort", cpNode.Line))
				}
			}
		}

		// protocol: optional string, TCP|UDP
		if prNode, ok := getMapValue(p, "protocol"); ok {
			if prNode.Kind != yaml.ScalarNode {
				errs = append(errs, newType("protocol", "string", prNode.Line))
			} else {
				if prNode.Value != "TCP" && prNode.Value != "UDP" {
					errs = append(errs, newUnsupported("protocol", prNode.Value, prNode.Line))
				}
			}
		}
	}

	return errs
}

// ---------- Probe / HTTPGetAction ----------

func validateProbe(node *yaml.Node, probeField string) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.MappingNode {
		errs = append(errs, newType(probeField, "object", node.Line))
		return errs
	}

	httpGetNode, ok := getMapValue(node, "httpGet")
	if !ok {
		// требуется httpGet
		errs = append(errs, newRequired("httpGet"))
		return errs
	}

	if httpGetNode.Kind != yaml.MappingNode {
		errs = append(errs, newType("httpGet", "object", httpGetNode.Line))
		return errs
	}

	// path
	pathNode, ok := getMapValue(httpGetNode, "path")
	if !ok {
		errs = append(errs, newRequired("path"))
	} else {
		if pathNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("path", "string", pathNode.Line))
		} else if len(pathNode.Value) == 0 || pathNode.Value[0] != '/' {
			errs = append(errs, newInvalidFormat("path", pathNode.Value, pathNode.Line))
		}
	}

	// port
	portNode, ok := getMapValue(httpGetNode, "port")
	if !ok {
		errs = append(errs, newRequired("port"))
	} else {
		if portNode.Kind != yaml.ScalarNode {
			errs = append(errs, newType("port", "int", portNode.Line))
		} else {
			p, err := strconv.Atoi(portNode.Value)
			if err != nil {
				errs = append(errs, newType("port", "int", portNode.Line))
			} else if p <= 0 || p >= 65536 {
				errs = append(errs, newOutOfRange("port", portNode.Line))
			}
		}
	}

	return errs
}

// ---------- ResourceRequirements ----------

var memoryRe = regexp.MustCompile(`^[0-9]+(Ki|Mi|Gi)$`)

func validateResources(node *yaml.Node) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.MappingNode {
		errs = append(errs, newType("resources", "object", node.Line))
		return errs
	}

	// limits: optional
	if limitsNode, ok := getMapValue(node, "limits"); ok {
		errs = append(errs, validateResourceMap(limitsNode, "resources.limits")...)
	}

	// requests: optional
	if reqNode, ok := getMapValue(node, "requests"); ok {
		errs = append(errs, validateResourceMap(reqNode, "resources.requests")...)
	}

	return errs
}

func validateResourceMap(node *yaml.Node, prefix string) []ValidationError {
	var errs []ValidationError

	if node.Kind != yaml.MappingNode {
		errs = append(errs, newType(prefix, "object", node.Line))
		return errs
	}

	for i := 0; i < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		switch k.Value {
		case "cpu":
			if v.Kind != yaml.ScalarNode {
				errs = append(errs, newType(prefix+".cpu", "int", v.Line))
				continue
			}
			if _, err := strconv.Atoi(v.Value); err != nil {
				errs = append(errs, newType(prefix+".cpu", "int", v.Line))
			}
		case "memory":
			if v.Kind != yaml.ScalarNode {
				errs = append(errs, newType(prefix+".memory", "string", v.Line))
				continue
			}
			if !memoryRe.MatchString(v.Value) {
				errs = append(errs, newInvalidFormat(prefix+".memory", v.Value, v.Line))
			}
		default:
			// неизвестный ресурс можно либо игнорировать, либо ругаться.
			// Официальное API допускает расширения, поэтому просто игнорируем.
		}
	}

	return errs
}

// ---------- main / CLI ----------

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: yamlvalid <path-to-yaml>")
		os.Exit(1)
	}

	fileName := os.Args[1]

	content, err := os.ReadFile(fileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read file '%s': %v\n", fileName, err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot unmarshal yaml: %v\n", fileName, err)
		os.Exit(1)
	}

	if len(root.Content) == 0 {
		fmt.Fprintf(os.Stderr, "%s: empty yaml document\n", fileName)
		os.Exit(1)
	}

	doc := root.Content[0]
	errs := validatePod(doc)

	if len(errs) > 0 {
		for _, e := range errs {
			if e.Line > 0 {
				fmt.Fprintf(os.Stderr, "%s:%d %s\n", fileName, e.Line, e.Text)
			} else {
				fmt.Fprintf(os.Stderr, "%s: %s\n", fileName, e.Text)
			}
		}
		os.Exit(1)
	}

	// успешная валидация
	os.Exit(0)
}
