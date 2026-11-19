// Package provision sets up a freshly started panel through its API: the
// superadmin account, the Xray keys, the config profile, the node, the host,
// the default squad and the token the subscription page uses. The four panel
// install flows used to carry a copy of this sequence each.
package provision

import (
	"fmt"
	"time"

	"github.com/FlexEbat/RemnaForge/internal/api"
	"github.com/FlexEbat/RemnaForge/internal/i18n"
	"github.com/FlexEbat/RemnaForge/internal/ui"
)

// sleep is replaced by tests; the panel needs a moment between some steps.
var sleep = time.Sleep

// Options says what differs between the installs.
type Options struct {
	DomainURL string // host:port of the panel, 127.0.0.1:3000 in the flows
	TargetDir string // the directory of docker-compose.yml

	Username, Password string // the superadmin account to register

	// SelfstealDomain names the config profile's inbounds and the host.
	SelfstealDomain string

	// PatchNodeKey is set when the node runs in the same compose file: the
	// panel's key is fetched and written into the node's SECRET_KEY, which
	// a panel without a node has no use for.
	PatchNodeKey bool

	// ProfileTag and Inbounds choose the config profile's inbounds. An
	// empty tag and the zero Inbounds give the stock Reality inbound.
	ProfileTag string
	Inbounds   api.ConfigProfileInbounds

	// CertFullchain and CertPrivkey are the node's certificate files for a
	// Hysteria2 inbound; they are empty when the inbound is not created.
	CertFullchain, CertPrivkey string

	// NodeAddress and NodeName override the node's defaults (172.30.0.1 and
	// "Steal"). A panel without a node on its host registers the remote
	// node under its selfsteal name; a co-located node keeps the defaults,
	// which match its docker network address.
	NodeAddress, NodeName string
}

// Run provisions the panel. It prints the same progress lines the flows
// printed and returns the first error.
func Run(o Options) error {
	// The flows print this password at the end; the log must not keep it.
	ui.RegisterSecret(o.Password)
	token := api.RegisterRemnawave(o.DomainURL, o.Username, o.Password, "")
	if token == "" {
		return fmt.Errorf("%s", i18n.T("ERROR_REGISTER"))
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("REGISTRATION_SUCCESS"), ui.ColorReset)

	if o.PatchNodeKey {
		fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_PUBLIC_KEY"), ui.ColorReset)
		sleep(1 * time.Second)
		if err := api.GetPublicKey(o.DomainURL, token, o.TargetDir); err != nil {
			return err
		}
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GENERATE_KEYS"), ui.ColorReset)
	sleep(1 * time.Second)
	privateKey := api.GenerateXrayKeys(o.DomainURL, token)
	if privateKey == "" {
		return fmt.Errorf("%s", i18n.T("ERROR_GENERATE_KEYS"))
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("GENERATE_KEYS_SUCCESS"), ui.ColorReset)

	// The panel ships a default profile that this install replaces.
	_ = api.DeleteConfigProfile(o.DomainURL, token, "")

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_CONFIG_PROFILE"), ui.ColorReset)
	configProfileUUID, inboundUUID := api.CreateConfigProfile(o.DomainURL, token, "StealConfig", o.SelfstealDomain,
		privateKey, o.ProfileTag, o.CertFullchain, o.CertPrivkey, o.Inbounds)
	if configProfileUUID == "" || inboundUUID == "" {
		return fmt.Errorf("%s", i18n.T("ERROR_CREATE_CONFIG_PROFILE"))
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("CONFIG_PROFILE_CREATED"), ui.ColorReset)

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_NODE"), ui.ColorReset)
	if err := api.CreateNode(o.DomainURL, token, configProfileUUID, inboundUUID, o.NodeAddress, o.NodeName); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATE_HOST"), ui.ColorReset)
	if err := api.CreateHost(o.DomainURL, token, inboundUUID, o.SelfstealDomain, configProfileUUID, ""); err != nil {
		return err
	}

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("GET_DEFAULT_SQUAD"), ui.ColorReset)
	squadUUIDs, _ := api.GetDefaultSquad(o.DomainURL, token)
	if len(squadUUIDs) > 0 {
		_ = api.UpdateSquad(o.DomainURL, token, squadUUIDs[0], inboundUUID)
	}
	fmt.Printf("%s%s%s\n", ui.ColorGreen, i18n.T("UPDATE_SQUAD"), ui.ColorReset)

	fmt.Printf("%s%s%s\n", ui.ColorYellow, i18n.T("CREATING_API_TOKEN"), ui.ColorReset)
	return api.CreateAPIToken(o.DomainURL, token, o.TargetDir, "")
}
