package proxy

import "fmt"

type nginx struct{}

func (nginx) Kind() Kind         { return Nginx }
func (nginx) ConfigFile() string { return "nginx.conf" }

func (nginx) Panel(p Params) (string, error) {
	if p.Layout == Socket {
		return fmt.Sprintf(nginxSocketTemplate,
			p.CookieName, p.CookieValue,
			p.PanelDomain, p.PanelCert,
			p.SubDomain, p.SubCert,
			p.SelfstealDomain, p.SelfstealCert,
		), nil
	}
	return fmt.Sprintf(nginxDirectTemplate,
		p.CookieName, p.CookieValue,
		p.PanelDomain, p.PanelCert,
		p.SubDomain, p.SubCert,
	), nil
}

func (nginx) Node(p Params) (string, error) {
	return fmt.Sprintf(nginxNodeTemplate, p.SelfstealDomain, p.SelfstealCert, p.SelfstealCert, p.SelfstealCert), nil
}

func (nginx) Extra(Params) map[string]string { return nil }
