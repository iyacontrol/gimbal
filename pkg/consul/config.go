package consul

type ConsulConfig struct {
	Addr   string
	Scheme string
	Token  string
	TLS    ConsulTlS
}

type ConsulTlS struct {
	KeyFile            string
	CertFile           string
	CAFile             string
	CAPath             string
	InsecureSkipVerify bool
}
