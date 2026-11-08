package domain

import (
	"strings"
	"testing"
	"time"
)

func TestGPUTypeHourlyPrice(t *testing.T) {
	tests := []struct {
		name      string
		gpu       GPUType
		wantPrice float64
		wantCloud Cloud
		rentable  bool
	}{
		{name: "community when offered there", gpu: GPUType{CommunityPrice: 0.34, SecurePrice: 0.69}, wantPrice: 0.34, wantCloud: CloudCommunity, rentable: true},
		{name: "secure when community does not offer it", gpu: GPUType{SecurePrice: 0.69}, wantPrice: 0.69, wantCloud: CloudSecure, rentable: true},
		{name: "not rentable without a price", gpu: GPUType{}, wantPrice: 0, wantCloud: CloudSecure, rentable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, cloud := tt.gpu.HourlyPrice()
			if price != tt.wantPrice || cloud != tt.wantCloud {
				t.Fatalf("HourlyPrice() = %v, %v, want %v, %v", price, cloud, tt.wantPrice, tt.wantCloud)
			}
			if tt.gpu.Rentable() != tt.rentable {
				t.Fatalf("Rentable() = %v, want %v", tt.gpu.Rentable(), tt.rentable)
			}
		})
	}
}

func TestCheckHourlyCap(t *testing.T) {
	tests := []struct {
		name      string
		price     float64
		maxHourly float64
		wantErr   bool
	}{
		{name: "no cap allows any price", price: 9.99, maxHourly: 0},
		{name: "a price at the cap is allowed", price: 1.5, maxHourly: 1.5},
		{name: "a price above the cap is refused", price: 1.51, maxHourly: 1.5, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckHourlyCap("A100", tt.price, tt.maxHourly)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckHourlyCap() = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "gpus.max_hourly") {
				t.Fatalf("CheckHourlyCap() = %v, want it to name gpus.max_hourly", err)
			}
		})
	}
}

func TestGPUInstanceUptimeAndSpend(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	running := GPUInstance{Status: "RUNNING", StartedAt: "2026-11-08T10:00:00Z", HourlyCost: 1.5}
	if got := running.Uptime(now); got != 2*time.Hour {
		t.Fatalf("Uptime() = %v, want 2h", got)
	}
	if got := running.SpentSoFar(now); got != 3 {
		t.Fatalf("SpentSoFar() = %v, want 3", got)
	}
	for name, instance := range map[string]GPUInstance{
		"stopped":           {Status: "EXITED", StartedAt: "2026-11-08T10:00:00Z", HourlyCost: 1.5},
		"never started":     {Status: "RUNNING", HourlyCost: 1.5},
		"started in future": {Status: "RUNNING", StartedAt: "2026-11-08T13:00:00Z", HourlyCost: 1.5},
	} {
		if instance.Uptime(now) != 0 || instance.SpentSoFar(now) != 0 {
			t.Fatalf("%s instance has uptime %v and spend %v, want none", name, instance.Uptime(now), instance.SpentSoFar(now))
		}
	}
}
