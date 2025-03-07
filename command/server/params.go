package server

import (
	"errors"
	"github.com/juanidrobo/polygon-edge/chain"
	"github.com/juanidrobo/polygon-edge/network"
	"github.com/juanidrobo/polygon-edge/secrets"
	"github.com/juanidrobo/polygon-edge/server"
	"github.com/hashicorp/go-hclog"
	"github.com/multiformats/go-multiaddr"
	"net"
)

const (
	configFlag            = "config"
	genesisPathFlag       = "chain"
	dataDirFlag           = "data-dir"
	libp2pAddressFlag     = "libp2p"
	prometheusAddressFlag = "prometheus"
	natFlag               = "nat"
	dnsFlag               = "dns"
	sealFlag              = "seal"
	maxPeersFlag          = "max-peers"
	maxInboundPeersFlag   = "max-inbound-peers"
	maxOutboundPeersFlag  = "max-outbound-peers"
	priceLimitFlag        = "price-limit"
	maxSlotsFlag          = "max-slots"
	blockGasTargetFlag    = "block-gas-target"
	secretsConfigFlag     = "secrets-config"
	restoreFlag           = "restore"
	blockTimeFlag         = "block-time"
	devIntervalFlag       = "dev-interval"
	devFlag               = "dev"
	corsOriginFlag        = "access-control-allow-origins"
)

const (
	unsetPeersValue = -1
)

var (
	params = &serverParams{
		rawConfig: &Config{
			Telemetry: &Telemetry{},
			Network:   &Network{},
			TxPool:    &TxPool{},
		},
	}
)

var (
	errInvalidPeerParams = errors.New("both max-peers and max-inbound/outbound flags are set")
	errInvalidNATAddress = errors.New("could not parse NAT IP address")
)

type serverParams struct {
	rawConfig  *Config
	configPath string

	libp2pAddress     *net.TCPAddr
	prometheusAddress *net.TCPAddr
	natAddress        net.IP
	dnsAddress        multiaddr.Multiaddr
	grpcAddress       *net.TCPAddr
	jsonRPCAddress    *net.TCPAddr

	blockGasTarget uint64
	devInterval    uint64
	isDevMode      bool

	corsAllowedOrigins []string

	genesisConfig *chain.Chain
	secretsConfig *secrets.SecretsManagerConfig
}

func (p *serverParams) validateFlags() error {
	// Validate the max peers configuration
	if p.isMaxPeersSet() && p.isPeerRangeSet() {
		return errInvalidPeerParams
	}

	return nil
}

func (p *serverParams) isMaxPeersSet() bool {
	return p.rawConfig.Network.MaxPeers != unsetPeersValue
}

func (p *serverParams) isPeerRangeSet() bool {
	return p.rawConfig.Network.MaxInboundPeers != unsetPeersValue ||
		p.rawConfig.Network.MaxOutboundPeers != unsetPeersValue
}

func (p *serverParams) isSecretsConfigPathSet() bool {
	return p.rawConfig.SecretsConfigPath != ""
}

func (p *serverParams) isPrometheusAddressSet() bool {
	return p.rawConfig.Telemetry.PrometheusAddr != ""
}

func (p *serverParams) isNATAddressSet() bool {
	return p.rawConfig.Network.NatAddr != ""
}

func (p *serverParams) isDNSAddressSet() bool {
	return p.rawConfig.Network.DNSAddr != ""
}

func (p *serverParams) isDevConsensus() bool {
	return server.ConsensusType(p.genesisConfig.Params.GetEngine()) == server.DevConsensus
}

func (p *serverParams) getRestoreFilePath() *string {
	if p.rawConfig.RestoreFile != "" {
		return &p.rawConfig.RestoreFile
	}

	return nil
}

func (p *serverParams) setRawGRPCAddress(grpcAddress string) {
	p.rawConfig.GRPCAddr = grpcAddress
}

func (p *serverParams) setRawJSONRPCAddress(jsonRPCAddress string) {
	p.rawConfig.JSONRPCAddr = jsonRPCAddress
}

func (p *serverParams) generateConfig() *server.Config {
	return &server.Config{
		Chain: p.genesisConfig,
		JSONRPC: &server.JSONRPC{
			JSONRPCAddr:              p.jsonRPCAddress,
			AccessControlAllowOrigin: p.corsAllowedOrigins,
		},
		GRPCAddr:   p.grpcAddress,
		LibP2PAddr: p.libp2pAddress,
		Telemetry: &server.Telemetry{
			PrometheusAddr: p.prometheusAddress,
		},
		Network: &network.Config{
			NoDiscover:       p.rawConfig.Network.NoDiscover,
			Addr:             p.libp2pAddress,
			NatAddr:          p.natAddress,
			DNS:              p.dnsAddress,
			DataDir:          p.rawConfig.DataDir,
			MaxPeers:         p.rawConfig.Network.MaxPeers,
			MaxInboundPeers:  p.rawConfig.Network.MaxInboundPeers,
			MaxOutboundPeers: p.rawConfig.Network.MaxOutboundPeers,
			Chain:            p.genesisConfig,
		},
		DataDir:        p.rawConfig.DataDir,
		Seal:           p.rawConfig.ShouldSeal,
		PriceLimit:     p.rawConfig.TxPool.PriceLimit,
		MaxSlots:       p.rawConfig.TxPool.MaxSlots,
		SecretsManager: p.secretsConfig,
		RestoreFile:    p.getRestoreFilePath(),
		BlockTime:      p.rawConfig.BlockTime,
		LogLevel:       hclog.LevelFromString(p.rawConfig.LogLevel),
	}
}
