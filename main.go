package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
	"github.com/cert-manager/cert-manager/pkg/issuer/acme/dns/util"
	"github.com/transip/gotransip/v6"
	"github.com/transip/gotransip/v6/domain"
	"github.com/transip/gotransip/v6/repository"
	v1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// GroupName is the API group name used to register the webhook with cert-manager
var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our custom DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&transipDNSProviderSolver{},
	)
}

// transipDNSProviderSolver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for the TransIP DNS provider.
type transipDNSProviderSolver struct {
	client *kubernetes.Clientset
}

// transipDNSProviderConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type transipDNSProviderConfig struct {
	AccountName         string               `json:"accountName"`
	PrivateKey          []byte               `json:"privateKey"`
	PrivateKeySecretRef v1.SecretKeySelector `json:"privateKeySecretRef"`
	TTL                 int                  `json:"ttl"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
// For example, `cloudflare` may be used as the name of a solver.
func (c *transipDNSProviderSolver) Name() string {
	return "transip"
}

func (c *transipDNSProviderSolver) NewTransipClient(ch *v1alpha1.ChallengeRequest, cfg *transipDNSProviderConfig) (*repository.Client, error) {
	privateKey := cfg.PrivateKey

	if len(privateKey) == 0 {
		secret, err := c.client.CoreV1().Secrets(ch.ResourceNamespace).Get(context.TODO(), cfg.PrivateKeySecretRef.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		ok := false
		privateKey, ok = secret.Data[cfg.PrivateKeySecretRef.Key]
		if !ok {
			return nil, fmt.Errorf("no private key for %q in secret '%s/%s'", cfg.PrivateKeySecretRef.Key, cfg.PrivateKeySecretRef.Name, ch.ResourceNamespace)
		}
	}

	klog.V(4).InfoS("Creating TransIP API client", "accountName", cfg.AccountName)

	client, err := gotransip.NewClient(gotransip.ClientConfiguration{
		AccountName:      cfg.AccountName,
		PrivateKeyReader: bytes.NewReader(privateKey),
	})
	if err != nil {
		return nil, err
	}

	return &client, nil
}

func (c *transipDNSProviderSolver) NewDNSEntryFromChallenge(ch *v1alpha1.ChallengeRequest, cfg *transipDNSProviderConfig, domainName string) domain.DNSEntry {
	return domain.DNSEntry{
		Name:    extractRecordName(ch.ResolvedFQDN, domainName),
		Expire:  cfg.TTL,
		Type:    "TXT",
		Content: ch.Key,
	}
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (c *transipDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	domainName := extractDomainName(ch.ResolvedZone)
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		klog.ErrorS(err, "Failed to load config")
		return err
	}

	client, err := c.NewTransipClient(ch, cfg)
	if err != nil {
		klog.ErrorS(err, "Failed to create TransIP client")
		return err
	}

	klog.InfoS("Presenting DNS record for ACME challenge", "fqdn", ch.ResolvedFQDN, "domain", domainName)

	domainRepo := domain.Repository{Client: *client}
	dnsEntries, err := domainRepo.GetDNSEntries(domainName)
	if err != nil {
		klog.ErrorS(err, "Failed to get DNS entries", "domain", domainName)
		return err
	}

	acmeDnsEntry := c.NewDNSEntryFromChallenge(ch, cfg, domainName)

	// This method should tolerate being called multiple times
	// with the same value. If a TXT record for this request
	// already exists, we'll simply exit.
	// Compare only name, type, and content (ignore TTL/Expire)
	for _, s := range dnsEntries {
		if s.Name == acmeDnsEntry.Name && s.Type == acmeDnsEntry.Type && s.Content == acmeDnsEntry.Content {
			klog.V(4).InfoS("ACME DNS entry already exists, skipping", "name", acmeDnsEntry.Name, "type", acmeDnsEntry.Type)
			return nil
		}
	}

	err = domainRepo.AddDNSEntry(domainName, acmeDnsEntry)
	if err != nil {
		klog.ErrorS(err, "Failed to add DNS entry", "domain", domainName, "name", acmeDnsEntry.Name)
		return err
	}

	klog.InfoS("DNS record created successfully", "name", acmeDnsEntry.Name, "type", acmeDnsEntry.Type, "ttl", acmeDnsEntry.Expire)

	return nil
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (c *transipDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	domainName := extractDomainName(ch.ResolvedZone)

	cfg, err := loadConfig(ch.Config)
	if err != nil {
		klog.ErrorS(err, "Failed to load config during cleanup")
		return err
	}

	client, err := c.NewTransipClient(ch, cfg)
	if err != nil {
		klog.ErrorS(err, "Failed to create TransIP client during cleanup")
		return err
	}

	klog.InfoS("Cleaning up DNS record for ACME challenge", "fqdn", ch.ResolvedFQDN, "domain", domainName)

	domainRepo := domain.Repository{Client: *client}
	dnsEntries, err := domainRepo.GetDNSEntries(domainName)
	if err != nil {
		klog.ErrorS(err, "Failed to get DNS entries during cleanup", "domain", domainName)
		return err
	}

	acmeDnsEntry := c.NewDNSEntryFromChallenge(ch, cfg, domainName)

	// If multiple TXT records exist with the same record name (e.g.
	// _acme-challenge.example.com) then **only** the record with the same `key`
	// value provided on the ChallengeRequest should be cleaned up.
	// Compare only name, type, and content (ignore TTL/Expire)
	for _, s := range dnsEntries {
		if s.Name == acmeDnsEntry.Name && s.Type == acmeDnsEntry.Type && s.Content == acmeDnsEntry.Content {
			klog.InfoS("Deleting DNS record", "name", s.Name, "type", s.Type)

			err = domainRepo.RemoveDNSEntry(domainName, acmeDnsEntry)
			if err != nil {
				klog.ErrorS(err, "Failed to remove DNS entry", "domain", domainName, "name", acmeDnsEntry.Name)
				return err
			}

			klog.InfoS("DNS record deleted successfully", "name", acmeDnsEntry.Name)
			return nil
		}
	}

	klog.V(4).InfoS("DNS record not found for cleanup", "name", acmeDnsEntry.Name, "type", acmeDnsEntry.Type)

	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (c *transipDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	c.client = cl

	///// END OF CODE TO MAKE KUBERNETES CLIENTSET AVAILABLE
	return nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (*transipDNSProviderConfig, error) {
	cfg := transipDNSProviderConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return &cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return &cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	return &cfg, nil
}

func extractRecordName(fqdn, domain string) string {
	if idx := strings.Index(fqdn, "."+domain); idx != -1 {
		return fqdn[:idx]
	}
	return util.UnFqdn(fqdn)
}

func extractDomainName(zone string) string {
	authZone, err := util.FindZoneByFqdn(context.TODO(), zone, util.RecursiveNameservers)
	if err != nil {
		klog.V(4).InfoS("Could not determine zone by FQDN, using provided zone", "zone", zone, "error", err.Error())
		return zone
	}
	return util.UnFqdn(authZone)
}
