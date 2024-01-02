package main

import (
	"fmt"
	"log"
	"reflect"
	"time"

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

component2 "yo2" {
	enabled = !component1_yo_exports_enabled
	message = component1_yo_exports_enabled ? "yo is enabled" : "yo is disabled"
}

component2 "yo3" {
	enabled = !component1_yo_exports_enabled
	message = "hi!"
	channel = component2_yo2_exports_channel
	function = component2_yo2_exports_function
}
`

// -------- Component Interfaces -----------

type ComponentConfig interface{}
type ComponentExports map[string]cty.Value

type Component interface {
	GetName() string
	Update(string, ComponentConfig)
	Run()
	GetExports() ComponentExports
}

// ----------- Component 1 --------------

type Component1Config struct {
	Enabled bool `hcl:"enabled,optional"`
}

type Component1 struct {
	Name    string
	Config  Component1Config
	Exports ComponentExports
}

func (c *Component1) Update(Name string, config ComponentConfig) {
	c.Name = Name
	c.Config = config.(Component1Config)

	// TODO: use cty tags to automatically export full "Exports" struct
	enabledType, err := gocty.ImpliedType(c.Config.Enabled)
	if err != nil {
		log.Fatal(err)
	}

	c.Exports = map[string]cty.Value{}

	val, _ := gocty.ToCtyValue(c.Config.Enabled, enabledType)
	c.Exports = map[string]cty.Value{
		"enabled": val,
	}
}

func (c *Component1) GetName() string {
	return c.Name
}

func (c *Component1) GetExports() ComponentExports {
	return c.Exports
}

func (c *Component1) Run() {
	fmt.Printf("Running component1: Name -> %s, Config -> %v, Exports -> %v\n", c.Name, c.Config, c.GetExports())
}

// ----------- Component 2 --------------

type Component2Config struct {
	Enabled       bool      `hcl:"enabled,optional"`
	Message       string    `hcl:"message,optional"`
	InputChannel  cty.Value `hcl:"channel,optional"`
	InputFunction cty.Value `hcl:"function,optional"`
}

type Component2 struct {
	Name    string
	Config  Component2Config
	Exports ComponentExports
}

func (c *Component2) Update(Name string, config ComponentConfig) {
	c.Name = Name
	c.Config = config.(Component2Config)

	outchannel := make(chan string, 100)
	chanty := cty.Capsule("outchannel", reflect.ChanOf(reflect.BothDir, reflect.TypeOf("")))

	c.Exports = map[string]cty.Value{}

	c.Exports["channel"] = cty.CapsuleVal(chanty, &outchannel)

	f := func() string {
		return fmt.Sprintf("hello from %v", c.Name)
	}
	functy := cty.Capsule("outfunc", reflect.FuncOf(nil, []reflect.Type{reflect.TypeOf("")}, false))
	c.Exports["function"] = cty.CapsuleVal(functy, &f)

	enabledType, err := gocty.ImpliedType(c.Config.Enabled)
	if err != nil {
		log.Fatal(err)
	}

	messageType, err := gocty.ImpliedType(c.Config.Message)
	if err != nil {
		log.Fatal(err)
	}

	enabled, _ := gocty.ToCtyValue(c.Config.Enabled, enabledType)
	message, _ := gocty.ToCtyValue(c.Config.Message, messageType)

	c.Exports["enabled"] = enabled
	c.Exports["message"] = message
}

func (c *Component2) GetName() string {
	return c.Name
}

func (c *Component2) GetExports() ComponentExports {
	return c.Exports
}

func (c *Component2) Run() {
	fmt.Printf("Running component2: Name -> %s, Config -> %v, Exports -> %v\n", c.Name, c.Config, c.Exports)
	if !c.Config.InputChannel.IsNull() {
		inChannel := c.Config.InputChannel.EncapsulatedValue().(*chan string)
		go func() {
			for {
				select {
				case msg := <-*inChannel:
					fmt.Printf("%v: Received message -> %s\n", c.Name, msg)
				}

			}
		}()
	}

	if !c.Config.InputFunction.IsNull() {
		inFunction := c.Config.InputFunction.EncapsulatedValue().(*func() string)
		go func() {
			for {
				fmt.Printf("%v: Calling function -> %s\n", c.Name, (*inFunction)())
				time.Sleep(1 * time.Second)
			}
		}()

	}

	if !c.Exports["channel"].IsNull() {
		outChannel := c.Exports["channel"].EncapsulatedValue().(*chan string)

		go func() {
			for {
				fmt.Printf("%v: Sending message to channel\n", c.Name)
				*outChannel <- fmt.Sprintf("A message from %v", c.Name)
				time.Sleep(1 * time.Second)
			}
		}()

	}
}

// -------- Component Schemas -----------

type Configuration struct {
	Remain hcl.Body `hcl:",remain"`
}

type ComponentNode struct {
	ID            string
	ComponentType reflect.Type
	ConfigType    reflect.Type
	Schema        hcl.BodySchema
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
	"component2": {
		ID:            "component2",
		ComponentType: reflect.TypeOf(Component2{}),
		ConfigType:    reflect.TypeOf(Component2Config{}),
		Schema: hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{
				{
					Type:       "component2",
					LabelNames: []string{"name"},
				},
			},
		},
	},
}

var configSchema hcl.BodySchema

func init() {
	for _, component := range componentNodeTypes {
		configSchema.Blocks = append(configSchema.Blocks, component.Schema.Blocks...)
	}
}

// -------- Global State -----------

var components = []Component{}

// -------- Main logic -----------

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

		for name, attr := range component.GetExports() {
			ctx.Variables[fmt.Sprintf("%v_%v_exports_%v", componentNode.ID, component.GetName(), name)] = attr
		}
	}

	for _, component := range components {
		component.Run()
	}

	time.Sleep(10 * time.Second)
}
