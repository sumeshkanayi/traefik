package triton

import (
	"context"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"text/template"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/containous/traefik/v2/pkg/config/dynamic"
	"github.com/containous/traefik/v2/pkg/job"
	"github.com/containous/traefik/v2/pkg/log"
	"github.com/containous/traefik/v2/pkg/provider"
	"github.com/containous/traefik/v2/pkg/safe"
	"github.com/containous/traefik/v2/pkg/types"
	triton "github.com/joyent/triton-go"
	"github.com/joyent/triton-go/authentication"
	"github.com/joyent/triton-go/compute"
)

// DefaultTemplateRule The default template for the default rule.
const DefaultTemplateRule = "Host(`{{ normalize .Name }}`)"

var _ provider.Provider = (*Provider)(nil)

type itemData struct {
	ID        string
	Name      string
	Address   string
	Port      string
	Labels    map[string]string
	ExtraConf configuration
}

// Provider holds configurations of the provider.
type Provider struct {
	Constraints       string          `description:"Constraints is an expression that Traefik matches against the container's labels to determine whether to create any route for that container." json:"constraints,omitempty" toml:"constraints,omitempty" yaml:"constraints,omitempty" export:"true"`
	Endpoint          *EndpointConfig `description:"Consul endpoint settings" json:"endpoint,omitempty" toml:"endpoint,omitempty" yaml:"endpoint,omitempty" export:"true"`
	Prefix            string          `description:"Prefix for consul service tags. Default 'traefik'" json:"prefix,omitempty" toml:"prefix,omitempty" yaml:"prefix,omitempty" export:"true"`
	RefreshInterval   types.Duration  `description:"Interval for check Consul API. Default 100ms" json:"refreshInterval,omitempty" toml:"refreshInterval,omitempty" yaml:"refreshInterval,omitempty" export:"true"`
	RequireConsistent bool            `description:"Forces the read to be fully consistent." json:"requireConsistent,omitempty" toml:"requireConsistent,omitempty" yaml:"requireConsistent,omitempty" export:"true"`
	Stale             bool            `description:"Use stale consistency for catalog reads." json:"stale,omitempty" toml:"stale,omitempty" yaml:"stale,omitempty" export:"true"`
	Cache             bool            `description:"Use local agent caching for catalog reads." json:"cache,omitempty" toml:"cache,omitempty" yaml:"cache,omitempty" export:"true"`
	ExposedByDefault  bool            `description:"Expose containers by default." json:"exposedByDefault,omitempty" toml:"exposedByDefault,omitempty" yaml:"exposedByDefault,omitempty" export:"true"`
	DefaultRule       string          `description:"Default rule." json:"defaultRule,omitempty" toml:"defaultRule,omitempty" yaml:"defaultRule,omitempty"`
	defaultRuleTpl    *template.Template
}

// EndpointConfig holds configurations of the endpoint.

type EndpointConfig struct {
	SDCAccount       string         `description:"SDC Account" json:"sdcaccount,omitempty" toml:"sdcaccount,omitempty" yaml:"sdaccount,omitempty" export:"true"`
	SDCKeyID         string         `description:"SDC Key ID" json:"sdckeyid,omitempty" toml:"sdckeyid,omitempty" yaml:"sdckeyid,omitempty" export:"true"`
	SDCKeyMaterial   string         `description:"SDC Key Material" json:"sdckeymaterial,omitempty" toml:"sdckeymaterial,omitempty" yaml:"sdckeymaterial,omitempty" export:"true"`
	Address          string         `description:"The address of the Consul server" json:"address,omitempty" toml:"address,omitempty" yaml:"address,omitempty" export:"true"`
	EndpointWaitTime types.Duration `description:"WaitTime limits how long a Watch will block. If not provided, the agent default values will be used" json:"endpointWaitTime,omitempty" toml:"endpointWaitTime,omitempty" yaml:"endpointWaitTime,omitempty" export:"true"`
}

// SetDefaults sets the default values.
func (c *EndpointConfig) SetDefaults() {
	c.Address = "http://127.0.0.1:8500"
}

// SetDefaults sets the default values.
func (p *Provider) SetDefaults() {
	endpoint := &EndpointConfig{}
	endpoint.SetDefaults()
	p.Endpoint = endpoint
	p.RefreshInterval = types.Duration(15 * time.Second)
	p.Prefix = "traefik"
	p.ExposedByDefault = true
	p.DefaultRule = DefaultTemplateRule
}

// Init the provider.
func (p *Provider) Init() error {
	defaultRuleTpl, err := provider.MakeDefaultRuleTemplate(p.DefaultRule, nil)
	if err != nil {
		return fmt.Errorf("error while parsing default rule: %v", err)
	}

	p.defaultRuleTpl = defaultRuleTpl

	return nil

}

// Provide allows the consul catalog provider to provide configurations to traefik using the given configuration channel.
func (p *Provider) Provide(configurationChan chan<- dynamic.Message, pool *safe.Pool) error {
	pool.GoCtx(func(routineCtx context.Context) {
		ctxLog := log.With(routineCtx, log.Str(log.ProviderName, "triton"))
		logger := log.FromContext(ctxLog)

		operation := func() error {

			ticker := time.NewTicker(time.Duration(p.RefreshInterval))

			for {
				select {
				case <-ticker.C:
					c2 := "https://us-east-2-cloudapi.bdf-cloud.iqvia.net"
					c3 := "https://us-east-3-cloudapi.bdf-cloud.iqvia.net"
					c4 := "https://us-east-4-cloudapi.bdf-cloud.iqvia.net"

					c2s, err1 := p.NewTritonClient(c2, ctxLog)
					c3s, err2 := p.NewTritonClient(c3, ctxLog)
					c4s, err3 := p.NewTritonClient(c4, ctxLog)

					if err1 != nil && err2 != nil && err3 != nil {
						logger.Fatalln("Error")

					}
					cArr := []*compute.ComputeClient{c2s, c3s, c4s}
					tagName := fmt.Sprintf(p.Prefix + ".Enable")
					data, err := p.GetMachineMetaByTag(tagName, "true", cArr, ctxLog)
					if err != nil {
						logger.Errorf("error triton meta data, %v", err)
						return err
					}

					configuration := p.buildConfiguration(routineCtx, data)
					fmt.Println("Build configuration is ", *configuration)
					configurationChan <- dynamic.Message{
						ProviderName:  "triton",
						Configuration: configuration,
					}
				case <-routineCtx.Done():
					ticker.Stop()
					return nil
				}
			}
		}

		notify := func(err error, time time.Duration) {
			logger.Errorf("Provider connection error %+v, retrying in %s", err, time)
		}

		err := backoff.RetryNotify(safe.OperationWithRecover(operation), backoff.WithContext(job.NewBackOff(backoff.NewExponentialBackOff()), ctxLog), notify)
		if err != nil {
			logger.Errorf("Cannot connect to triton server %+v", err)
		}
	})

	return nil
}

/*
func (p *Provider) getConsulServicesData(ctx context.Context) ([]itemData, error) {
	consulServiceNames, err := p.fetchServices(ctx)
	fmt.Println("Sumesh: consul service names are", consulServiceNames)
	if err != nil {
		return nil, err
	}

	var data []itemData
	for _, name := range consulServiceNames {
		consulServices, healthServices, err := p.fetchService(ctx, name)
		if err != nil {
			return nil, err
		}

		for i, consulService := range consulServices {
			address := consulService.ServiceAddress
			if address == "" {
				address = consulService.Address
			}

			item := itemData{
				ID:      consulService.ServiceID,
				Node:    consulService.Node,
				Name:    consulService.ServiceName,
				Address: address,
				Port:    strconv.Itoa(consulService.ServicePort),
				Labels:  tagsToNeutralLabels(consulService.ServiceTags, p.Prefix),
				Tags:    consulService.ServiceTags,
				Status:  healthServices[i].Checks.AggregatedStatus(),
			}

			fmt.Println("Sumesh Item Data is ", item)

			extraConf, err := p.getConfiguration(item)
			fmt.Println("Sumesh :Extra config is ", extraConf)
			if err != nil {
				log.FromContext(ctx).Errorf("Skip item %s: %v", item.Name, err)
				continue
			}
			item.ExtraConf = extraConf

			data = append(data, item)
		}
	}
	fmt.Println("Sumesh : data is ", data)
	return data, nil
}

func (p *Provider) fetchService(ctx context.Context, name string) ([]*api.CatalogService, []*api.ServiceEntry, error) {
	var tagFilter string
	if !p.ExposedByDefault {
		tagFilter = p.Prefix + ".enable=true"
	}

	opts := &api.QueryOptions{AllowStale: p.Stale, RequireConsistent: p.RequireConsistent, UseCache: p.Cache}

	consulServices, _, err := p.client.Catalog().Service(name, tagFilter, opts)
	if err != nil {
		return nil, nil, err
	}

	healthServices, _, err := p.client.Health().Service(name, tagFilter, false, opts)
	return consulServices, healthServices, err
}

func contains(values []string, val string) bool {
	for _, value := range values {
		if strings.EqualFold(value, val) {
			return true
		}
	}
	return false
}
*/
func (p *Provider) NewTritonClient(SDC_URL string, ctxLog context.Context) (*compute.ComputeClient, error) {
	logger := log.FromContext(ctxLog)
	keyMaterial := p.Endpoint.SDCKeyMaterial
	keyID := p.Endpoint.SDCKeyID
	accountName := p.Endpoint.SDCAccount
	fmt.Println("Triton details", keyMaterial, keyID, accountName)
	userName := ""
	var signer authentication.Signer
	var err error

	var keyBytes []byte
	if _, err = os.Stat(keyMaterial); err == nil {
		keyBytes, err = ioutil.ReadFile(keyMaterial)
		if err != nil {
			logger.Fatalln("Error reading key material")

		}
		block, _ := pem.Decode(keyBytes)
		if block == nil {
			logger.Fatalln("No key found")
		}

		if block.Headers["Proc-Type"] == "4,ENCRYPTED" {
			logger.Fatalln("password protected keys are not currently supported. Please decrypt the key prior to use")

		}

	} else {
		keyBytes = []byte(keyMaterial)
	}

	input := authentication.PrivateKeySignerInput{
		KeyID:              keyID,
		PrivateKeyMaterial: keyBytes,
		AccountName:        accountName,
		Username:           userName,
	}

	signer, err = authentication.NewPrivateKeySigner(input)
	if err != nil {
		logger.Fatalln("Error Creating SSH Private Key Signer")

	}

	config := &triton.ClientConfig{
		TritonURL:   SDC_URL,
		AccountName: accountName,
		Username:    userName,
		Signers:     []authentication.Signer{signer},
	}

	c, err := compute.NewClient(config)
	if err != nil {
		logger.Fatalln("Compute new client")

	}
	return c, err
}

func (p *Provider) GetMachineMetaByTag(tagName string, tagValue string, cArr []*compute.ComputeClient, ctxLog context.Context) ([]itemData, error) {
	var data []itemData
	logger := log.FromContext(ctxLog)
	var err error
	fmt.Println("C ARRRRR is", cArr)
	for _, c := range cArr {

		listInput := &compute.ListInstancesInput{Tags: map[string]interface{}{tagName: tagValue}}
		ci := c.Instances()
		instances, err := ci.List(context.Background(), listInput)
		if err != nil {
			logger.Fatalln("Error listing")

		}

		if len(instances) > 0 {
			for _, instance := range instances {
				ltinput := &compute.ListTagsInput{
					ID: instance.ID,
				}
				tags, err := ci.ListTags(ctxLog, ltinput)
				fmt.Println("Tags are ", tags, err, instance.ID)
				labels := make(map[string]string)
				for k, v := range tags {
					fmt.Println("KV", k, v)
					key := fmt.Sprintf("%v", k)
					val := fmt.Sprintf("%v", v)
					labels[key] = val
				}
				fmt.Println("Labels are ", labels, labels["traefik.triton.service.name"])
				tempData := itemData{
					ID:      instance.ID,
					Name:    labels["traefik.triton.service.name"],
					Address: instance.PrimaryIP,
					Port:    labels["traefik.triton.service.port"],
					Labels:  labels,
				}
				extraConf, _ := p.getConfiguration(tempData)

				tempData.ExtraConf = extraConf

				data = append(data, tempData)

			}

		}

	}

	return data, err
}
