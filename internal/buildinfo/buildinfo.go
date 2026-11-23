// Package buildinfo contains the development version of agentport.
//
// Release builds replace Version through Go linker flags using the Git tag.
package buildinfo

// Version is "0.1.0-dev" in development builds. The value inherited from the
// AgentClip project (0.7.x) was wrong for agentport, and "dev" keeps the release
// check from comparing a development build with real releases.
var Version = "0.1.0-dev"
