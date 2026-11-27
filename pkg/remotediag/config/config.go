package config

type Config struct {
	Enable             bool
	EnableTerminal     bool
	RDPGatewayAddress  string
	ConnectionsFile    string
	KeyFile            string
	TimeoutSeconds     int
	MaxOutputBytes     int
	MaxConcurrent      int
	HuaweiReadCommands []string
}
