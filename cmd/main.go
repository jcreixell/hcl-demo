package main

import (
	"fmt"
	"log"
	"reflect"

	"github.com/hashicorp/hcl2/gohcl"
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
)

var testConfig = `
component1 "yo" {
	enabled = true
}

component1 "yo2" {
	enabled = component1_yo_exports_enabled
}
`

// Configuration is the top-level struct that contains attributes and blocks defined within
// an `hcl` file.
type Configuration struct {
	Remain hcl.Body `hcl:",remain"`
}

type ComponentNode struct {
	ID            string
	ComponentType reflect.Type
	ConfigType    reflect.Type
	Schema        hcl.BodySchema
}

type ComponentConfig interface{}

type Component interface {
	GetID() string
	Update(string, ComponentConfig)
	Run()
	Exports() map[string]cty.Value
}

type Component1Config struct {
	Enabled bool `hcl:"enabled,optional" cty:"enabled"`
}

type Component1 struct {
	ID     string
	Config Component1Config
}

func (c *Component1) Update(ID string, config ComponentConfig) {
	c.ID = ID
	c.Config = config.(Component1Config)
}

func (c *Component1) GetID() string {
	return c.ID
}

func (c *Component1) Run() {
	fmt.Printf("Component1: ID -> %s, Config -> %v\n", c.ID, c.Config)
}

func (c *Component1) Exports() map[string]cty.Value {
	enabledType, err := gocty.ImpliedType(c.Config.Enabled)
	if err != nil {
		log.Fatal(err)
	}

	val, _ := gocty.ToCtyValue(c.Config.Enabled, enabledType)
	return map[string]cty.Value{
		"enabled": val,
	}
}

var componentNodeTypes = map[string]ComponentNode{
	"component1": {
		ID:            "component1",
		ComponentType: reflect.TypeOf(Component1{}),
		ConfigType:    reflect.TypeOf(Component1Config{}),
		Schema: hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{
				{
					Type:       "component1",
					LabelNames: []string{"name"},
				},
			},
		},
	},
}

var components = []Component{}

var configSchema hcl.BodySchema

func init() {
	for _, component := range componentNodeTypes {
		configSchema.Blocks = append(configSchema.Blocks, component.Schema.Blocks...)
	}
}

func main() {
	parser := hclparse.NewParser()

	file, diags := parser.ParseHCL([]byte(testConfig), "testConfig")
	if diags.HasErrors() {
		log.Fatal(diags)
	}

	var config Configuration

	confDiags := gohcl.DecodeBody(file.Body, nil, &config)
	if confDiags.HasErrors() {
		log.Fatal(confDiags)
	}

	content, remainDiags := config.Remain.Content(&configSchema)
	diags = diags.Extend(remainDiags)
	if diags.HasErrors() {
		log.Fatal(diags.Errs())
	}

	var ctx = hcl.EvalContext{
		Variables: map[string]cty.Value{},
	}
	for _, block := range content.Blocks {
		componentNode, ok := componentNodeTypes[block.Type]
		if !ok {
			log.Fatalf("unknown component type %q", block.Type)
		}

		if len(block.Labels) != 1 {
			log.Fatalf("expected exactly one label for component %q", block.Type)
		}

		component := reflect.New(componentNode.ComponentType).Interface().(Component)
		componentConfig := reflect.New(componentNode.ConfigType).Interface()

		if err := gohcl.DecodeBody(block.Body, &ctx, componentConfig); err != nil {
			log.Fatal(err)
		}

		component.Update(block.Labels[0], reflect.ValueOf(componentConfig).Elem().Interface())
		components = append(components, component)

		for name, attr := range component.Exports() {
			ctx.Variables[fmt.Sprintf("%v_%v_exports_%v", componentNode.ID, component.GetID(), name)] = attr
		}
	}

	for _, component := range components {
		component.Run()
	}
}
