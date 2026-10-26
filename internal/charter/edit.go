package charter

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"
)

// AddAllowedHost adds host to the charter file's permissions.network.allow and bumps
// its version. Comments and layout are kept. The result is re-validated before the
// file is replaced, so a bad edit never reaches disk.
func AddAllowedHost(path, host string) error {
	if !ValidHost(host) {
		return fmt.Errorf("%q is not a host or *.host", host)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 {
		return errors.New("empty charter")
	}
	root := doc.Content[0]
	allow := mapPath(root, "permissions", "network", "allow")
	if allow == nil || allow.Kind != yaml.SequenceNode {
		return errors.New("charter has no permissions.network.allow list")
	}
	for _, n := range allow.Content {
		if n.Value == AllowAll {
			return errors.New(`charter already allows every host ("*")`)
		}
		if n.Value == host {
			return nil
		}
	}
	item := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: host}
	if host[0] == '*' {
		item.Style = yaml.DoubleQuotedStyle
	}
	allow.Content = append(allow.Content, item)
	bumpVersion(root)

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if _, err := Parse(out.Bytes()); err != nil {
		return fmt.Errorf("edited charter is invalid: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// mapPath walks mapping keys from n and returns the final value node.
func mapPath(n *yaml.Node, keys ...string) *yaml.Node {
	for _, k := range keys {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		i := slices.IndexFunc(n.Content, func(c *yaml.Node) bool { return c.Value == k })
		if i < 0 || i%2 != 0 || i+1 >= len(n.Content) {
			return nil
		}
		n = n.Content[i+1]
	}
	return n
}

func bumpVersion(root *yaml.Node) {
	if v := mapPath(root, "version"); v != nil {
		if n, err := strconv.Atoi(v.Value); err == nil {
			v.Value = strconv.Itoa(n + 1)
		}
		return
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "version"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "2"})
}
