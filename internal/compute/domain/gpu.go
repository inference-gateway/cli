package domain

import (
	"fmt"
	"time"
)

// InstanceNamePrefix marks every GPU instance infer rents, so a listing shows
// infer's instances and never the rest of the account.
const InstanceNamePrefix = "infer-"

// Cloud is where a provider rents a GPU from: a community host or its own
// secure data centres.
type Cloud string

const (
	CloudCommunity Cloud = "COMMUNITY"
	CloudSecure    Cloud = "SECURE"
)

// GPUType is a GPU a provider rents, with its live hourly prices on each cloud.
// A zero price means the cloud does not offer it.
type GPUType struct {
	ID             string
	DisplayName    string
	MemoryGB       int
	CommunityPrice float64
	SecurePrice    float64
}

// HourlyPrice is what an hour of the GPU costs and on which cloud: community
// when it is offered there, else secure.
func (t GPUType) HourlyPrice() (float64, Cloud) {
	if t.CommunityPrice > 0 {
		return t.CommunityPrice, CloudCommunity
	}
	return t.SecurePrice, CloudSecure
}

// Rentable reports whether any cloud offers the GPU at a price.
func (t GPUType) Rentable() bool {
	return t.CommunityPrice > 0 || t.SecurePrice > 0
}

// GPUInstance is one rented GPU, billed by the hour while it runs.
type GPUInstance struct {
	ID         string
	Name       string
	Image      string
	Status     string
	HourlyCost float64
	StartedAt  string
}

// Running reports whether the provider has started the instance.
func (i GPUInstance) Running() bool {
	return i.Status == "RUNNING"
}

// Uptime is how long a running instance has been up at now, zero when it is
// not running or its start time is unknown.
func (i GPUInstance) Uptime(now time.Time) time.Duration {
	started, err := time.Parse(time.RFC3339, i.StartedAt)
	if !i.Running() || err != nil || started.After(now) {
		return 0
	}
	return now.Sub(started)
}

// SpentSoFar is what the instance has cost at now since it last started.
func (i GPUInstance) SpentSoFar(now time.Time) float64 {
	return i.Uptime(now).Hours() * i.HourlyCost
}

// ProvisionRequest is the GPU instance to rent and what it runs.
type ProvisionRequest struct {
	Name      string
	GPUTypeID string
	Image     string
	StartCmd  []string
	Cloud     Cloud
	DiskGB    int
	Env       map[string]string
	Ports     []string
}

// CheckHourlyCap refuses a GPU whose hourly price is above the cap, so a
// rental never costs more than the user allowed. A zero cap allows any price.
func CheckHourlyCap(gpuType string, price, maxHourly float64) error {
	if maxHourly > 0 && price > maxHourly {
		return fmt.Errorf("%s costs $%.2f/hr, above gpus.max_hourly ($%.2f)", gpuType, price, maxHourly)
	}
	return nil
}
