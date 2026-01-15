package registry

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/docker/docker/api/types/network"
	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

const (
	dockerNetworkFixedIPOffsetBase   = 200 * 256
	localRegistryIPOffset    = dockerNetworkFixedIPOffsetBase + 1
	localMirrorIPOffsetStart = dockerNetworkFixedIPOffsetBase + 2
	localMirrorIPOffsetRange = 200
)

func ResolveLocalRegistryIP() (string, error) {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		return "", errors.Wrap(err, "unable to create docker client")
	}

	networkInfo, err := cli.NetworkInspect(ctx, KindNetworkName, network.InspectOptions{})
	if err != nil {
		return "", errors.Wrap(err, "unable to inspect kind network")
	}

	prefix, gateway, err := dockerNetworkIPv4Prefix(KindNetworkName, networkInfo)
	if err != nil {
		return "", err
	}

	registryIP, err := fixedIPForOffset(KindNetworkName, prefix, gateway, networkInfo.Containers, localRegistryIPOffset, EducatesRegistryContainer)
	if err != nil {
		return "", errors.Wrap(err, "unable to resolve fixed kind IP for registry")
	}

	return registryIP.String(), nil
}

func ResolveLocalMirrorIP(containerName string) (string, error) {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		return "", errors.Wrap(err, "unable to create docker client")
	}

	networkInfo, err := cli.NetworkInspect(ctx, KindNetworkName, network.InspectOptions{})
	if err != nil {
		return "", errors.Wrap(err, "unable to inspect kind network")
	}

	prefix, gateway, err := dockerNetworkIPv4Prefix(KindNetworkName, networkInfo)
	if err != nil {
		return "", err
	}

	offset, err := mirrorOffsetForContainer(containerName)
	if err != nil {
		return "", err
	}

	for i := uint32(0); i < localMirrorIPOffsetRange; i++ {
		candidateOffset := localMirrorIPOffsetStart + ((offset + i) % localMirrorIPOffsetRange)
		if candidateOffset == localRegistryIPOffset {
			continue
		}

		candidateIP, available, err := candidateFixedIP(KindNetworkName, prefix, gateway, networkInfo.Containers, candidateOffset, containerName)
		if err != nil {
			return "", err
		}
		if !available {
			continue
		}
		return candidateIP.String(), nil
	}

	return "", errors.New("unable to allocate fixed kind IP for mirror")
}

func EnsureContainerKindNetworkIP(containerName string, fixedIP string) error {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerInfo, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return errors.Wrap(err, "unable to inspect container")
	}

	if kindNetwork, exists := containerInfo.NetworkSettings.Networks[KindNetworkName]; exists {
		if fixedIP == "" || kindNetwork.IPAddress == fixedIP {
			return nil
		}
	}

	cli.NetworkDisconnect(ctx, KindNetworkName, containerName, false)

	endpointSettings := &network.EndpointSettings{}
	if fixedIP != "" {
		endpointSettings.IPAddress = fixedIP
		endpointSettings.IPAMConfig = &network.EndpointIPAMConfig{
			IPv4Address: fixedIP,
		}
	}

	if err := cli.NetworkConnect(ctx, KindNetworkName, containerName, endpointSettings); err != nil {
		return errors.Wrapf(err, "unable to connect container to %s network", KindNetworkName)
	}

	return nil
}
func dockerNetworkIPv4Prefix(networkName string, networkInfo network.Inspect) (netip.Prefix, *netip.Addr, error) {
	for _, config := range networkInfo.IPAM.Config {
		if config.Subnet == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(config.Subnet)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}

		var gateway *netip.Addr
		if config.Gateway != "" {
			if addr, err := netip.ParseAddr(config.Gateway); err == nil && addr.Is4() {
				gateway = &addr
			}
		}

		return prefix.Masked(), gateway, nil
	}

	return netip.Prefix{}, nil, errors.Errorf( "%s network has no IPv4 subnet", networkName)
}

func fixedIPForOffset(networkName string, prefix netip.Prefix, gateway *netip.Addr, containers map[string]network.EndpointResource, offset uint32, allowedContainerName string) (netip.Addr, error) {
	addr, available, err := candidateFixedIP(networkName, prefix, gateway, containers, offset, allowedContainerName)
	if err != nil {
		return netip.Addr{}, err
	}
	if !available {
		return netip.Addr{}, fmt.Errorf("%s network already uses fixed IP %s", networkName, addr.String())
	}
	return addr, nil
}

func candidateFixedIP(networkName string, prefix netip.Prefix, gateway *netip.Addr, containers map[string]network.EndpointResource, offset uint32, allowedContainerName string) (netip.Addr, bool, error) {
	base := prefix.Addr()
	if !base.Is4() {
		return netip.Addr{}, false, errors.New("kind network base is not IPv4")
	}

	addr, err := addIPv4Offset(base, offset)
	if err != nil {
		return netip.Addr{}, false, err
	}

	if !prefix.Contains(addr) {
		return netip.Addr{}, false, fmt.Errorf("%s network does not include fixed IP %s", networkName, addr.String())
	}

	if gateway != nil && *gateway == addr {
		return netip.Addr{}, false, fmt.Errorf("%s network gateway conflicts with fixed IP %s", networkName, addr.String())
	}

	if containerName, inUse := containerNameForIP(containers, addr); inUse {
		if allowedContainerName != "" && containerName == allowedContainerName {
			return addr, true, nil
		}
		return addr, false, nil
	}

	return addr, true, nil
}

func addIPv4Offset(base netip.Addr, offset uint32) (netip.Addr, error) {
	if !base.Is4() {
		return netip.Addr{}, errors.New("base address is not IPv4")
	}

	baseBytes := base.As4()
	baseValue := binary.BigEndian.Uint32(baseBytes[:])
	targetValue := baseValue + offset

	if targetValue < baseValue {
		return netip.Addr{}, errors.New("fixed IP offset overflows IPv4 range")
	}

	var targetBytes [4]byte
	binary.BigEndian.PutUint32(targetBytes[:], targetValue)

	return netip.AddrFrom4(targetBytes), nil
}


func containerNameForIP(containers map[string]network.EndpointResource, addr netip.Addr) (string, bool) {
	for _, container := range containers {
		if container.IPv4Address == "" {
			continue
		}

		parsed, err := netip.ParsePrefix(container.IPv4Address)
		if err != nil {
			continue
		}

		if parsed.Addr() == addr {
			return container.Name, true
		}
	}

	return "", false
}

func mirrorOffsetForContainer(containerName string) (uint32, error) {
	hash := fnv32a(containerName)
	return hash % localMirrorIPOffsetRange, nil
}

func fnv32a(value string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)

	hash := uint32(offset32)
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= prime32
	}
	return hash
}
