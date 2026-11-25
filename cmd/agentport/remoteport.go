package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rafaelb13/agentport/internal/agents"
	"github.com/rafaelb13/agentport/internal/companion"
	"github.com/rafaelb13/agentport/internal/remote"
)

// ownPortRechecks is how many times the server is asked again about a port this
// profile used before: our own old tunnel may still hold it for a moment.
const ownPortRechecks = 3

// chooseRemotePort checks the server for the remote loopback port and returns the
// one to use: preferred when free (or when the server cannot say), the next free
// one when it is busy and was not asked for, an error when it is busy and was.
func chooseRemotePort(destination, identityFile string, preferred int, explicit, owned bool) (int, error) {
	recheck := 0
	if owned {
		recheck = ownPortRechecks
	}
	query := func() (map[int]bool, bool) {
		return remote.ListeningPorts(context.Background(), destination, identityFile)
	}
	return remote.ChoosePort(query, preferred, explicit, recheck, func() { time.Sleep(time.Second) })
}

// choosePairPort is chooseRemotePort for `pair`, which has no setup run around
// it: it reuses the SSH identity and the port of an existing profile of the name.
func choosePairPort(name, destination string, port int, explicit bool) (int, error) {
	identityFile, owned := "", false
	if existing, err := companion.LoadProfile(name); err == nil {
		identityFile = existing.SSHIdentityFile
		if !explicit {
			port = existing.RemotePort
		}
		owned = existing.RemotePort == port
	}
	return chooseRemotePort(destination, identityFile, port, explicit, owned)
}

// healConfigureTimeout bounds the server-side reconfiguration, so a stalled ssh
// cannot hold the tunnel loop.
const healConfigureTimeout = 90 * time.Second

// healRemotePort is the companion's answer to a refused reverse forward: another
// process holds profile.RemotePort on the server. It picks a free port, saves it
// in the profile and points the server's agents at it, then returns the profile
// the tunnel should use. When the server does not list the port as busy, the
// refusal is not about the port (for instance TCP forwarding is disabled), so
// the port is kept and an error says why.
func healRemotePort(ctx context.Context, profile companion.Profile) (companion.Profile, error) {
	listening, known := remote.ListeningPorts(ctx, profile.Destination, profile.SSHIdentityFile)
	if known && !listening[profile.RemotePort] {
		return profile, fmt.Errorf("the server does not list port %d as in use, so it was kept; check that the server allows TCP forwarding", profile.RemotePort)
	}
	port, err := remote.PickPort(listening, known, profile.RemotePort, false, profile.RemotePort)
	if err != nil {
		return profile, err
	}
	next := profile
	next.RemotePort = port
	if err := companion.SaveProfile(next); err != nil {
		return profile, fmt.Errorf("save the new remote port: %w", err)
	}
	if next.Agent == companion.AgentNone {
		return next, nil
	}
	selection := next.Agent
	if selection == "" {
		selection = "all"
	}
	done := make(chan error, 1)
	go func() {
		_, err := agents.Configure(next, selection)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return next, fmt.Errorf("saved port %d but could not update the agents on the server (run `agentport connect %s`): %w", port, next.Name, err)
		}
	case <-time.After(healConfigureTimeout):
		return next, fmt.Errorf("saved port %d but updating the agents on the server timed out (run `agentport connect %s`)", port, next.Name)
	case <-ctx.Done():
		return next, errors.New("stopped while updating the agents on the server")
	}
	return next, nil
}
