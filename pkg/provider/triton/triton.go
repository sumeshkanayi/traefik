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
	Endpoint          *EndpointConfig `description:"Consul endpoint settings" json:"endpoint,omitempty" toml:"endpoint,omitempty" yaml:"endpoint,omitempty" export:"true"`
	Prefix            string          `description:"Prefix for ctriton instance tags. Default 'traefik'" json:"prefix,omitempty" toml:"prefix,omitempty" yaml:"prefix,omitempty" export:"true"`
	RefreshInterval   types.Duration  `description:"Interval for check Consul API. Default 100ms" json:"refreshInterval,omitempty" toml:"refreshInterval,omitempty" yaml:"refreshInterval,omitempty" export:"true"`
	ExposedByDefault  bool            `description:"Expose containers by default." json:"exposedByDefault,omitempty" toml:"exposedByDefault,omitempty" yaml:"exposedByDefault,omitempty" export:"true"`
	DefaultRule       string          `description:"Default rule." json:"defaultRule,omitempty" toml:"defaultRule,omitempty" yaml:"defaultRule,omitempty"`
	defaultRuleTpl    *template.Template
}

// EndpointConfig holds configurations of the endpoint.

type EndpointConfig struct {
	SDCAccount       string         `description:"SDC Account" json:"sdcaccount,omitempty" toml:"sdcaccount,omitempty" yaml:"sdaccount,omitempty" export:"true"`
	SDCKeyID         string         `description:"SDC Key ID" json:"sdckeyid,omitempty" toml:"sdckeyid,omitempty" yaml:"sdckeyid,omitempty" export:"true"`
	SDCKeyMaterial   string         `description:"SDC Key Material" json:"sdckeymaterial,omitempty" toml:"sdckeymaterial,omitempty" yaml:"sdckeymaterial,omitempty" export:"true"`
	SDCCloudAPIs     string         `description:"SDC CLoudAPIs" json:"cloudapis,omitempty" toml:"cloudapis,omitempty" yaml:"cloudapis,omitempty" export:"true"`
	EndpointWaitTime types.Duration `description:"WaitTime limits how long a Watch will block. If not provided, the agent default values will be used" json:"endpointWaitTime,omitempty" toml:"endpointWaitTime,omitempty" yaml:"endpointWaitTime,omitempty" export:"true"`
}

// SetDefaults sets the default values.
func (c *EndpointConfig) SetDefaults() {
	c.SDCKeyMaterial = os.Getenv("HOME")+"/.ssh/id_rsa"
	c.SDCKeyID=os.Getenv("SDC_KEY_ID")
	c.SDCAccount=os.Getenv("SDC_ACCOUNT")
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
					var computeClientArray []*compute.ComputeClient
					for _,api:=range p.Endpoint.SDCCloudAPIs{
						cClient,err:=p.NewTritonClient(api, ctxLog)
						if(err!=nil){
							logger.Errorf("Cannot connect to triton cloud api %+v", api)
							continue
						}
						computeClientArray=append(computeClientArray,cClient)
						

					}


					tagName := fmt.Sprintf(p.Prefix + ".Enable")
					data, err := p.GetTritonInstanceDetailsByTag(tagName, "true", computeClientArray, ctxLog)
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
			logger.Errorf("Cannot connect to triton cloud api %+v", err)
		}
	})

	return nil
}


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

func (p *Provider) GetTritonInstanceDetailsByTag(tagName string, tagValue string, cArr []*compute.ComputeClient, ctxLog context.Context) ([]itemData, error) {
	var data []itemData
	logger := log.FromContext(ctxLog)
	var err error
	for _, c := range cArr {

		listInput := &compute.ListInstancesInput{Tags: map[string]interface{}{tagName: tagValue}}
		ci := c.Instances()
		instances, err := ci.List(context.Background(), listInput)
		if err != nil {
			logger.Errorf("Cannot connect List instances %+v", err)
			continue

		}

		if len(instances) > 0 {
			for _, instance := range instances {
				ltinput := &compute.ListTagsInput{
					ID: instance.ID,
				}
				tags, err := ci.ListTags(ctxLog, ltinput)
				if (err!=nil){
					logger.Errorf("Cannot connect List tags %+v", err)
					continue

				}
				labels := make(map[string]string)
				for k, v := range tags {
					key := fmt.Sprintf("%v", k)
					val := fmt.Sprintf("%v", v)
					labels[key] = val
				}
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
