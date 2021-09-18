package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go"
	goauth "golang.org/x/oauth2/google"
	gcompute "google.golang.org/api/compute/v1"
	goption "google.golang.org/api/option"
)

const USER_AGENT string = "cloud-vm-dns-sync/0"

// Fetch the machine list, and for each machines, fetch its IP and PTR record
// setting.
//
// The PTR record for a machine can be set in the network interface tab. It
// should be the domain name that is supposed to point to the machine.
//
// Returns either an error, or a map from PTR domain name to IP addresses, or to
// the empty string if the machine is off.
func get_all_machines_ip(ctx context.Context) (res map[string]string, err error) {
	creds, err := goauth.FindDefaultCredentials(ctx)
	if err != nil {
		return
	}
	project_id := creds.ProjectID
	compute, err := gcompute.NewService(ctx, goption.WithUserAgent(USER_AGENT))
	if err != nil {
		return
	}
	instances := compute.Instances
	instance_list, err := instances.AggregatedList(project_id).Context(ctx).Do()
	if err != nil {
		return
	}
	arr := make([]*gcompute.Instance, 0)
	for _, l := range instance_list.Items {
		for _, inst := range l.Instances {
			arr = append(arr, inst)
		}
	}
	res = make(map[string]string)
	for _, inst := range arr {
		nics := inst.NetworkInterfaces
		if len(nics) == 0 {
			continue
		}
		for _, nic := range nics {
			if len(nic.AccessConfigs) != 1 {
				continue
			}
			ass := nic.AccessConfigs[0]
			ptr := ass.PublicPtrDomainName
			if ptr == "" {
				continue
			}
			ip := ass.NatIP
			res[ptr] = ip
		}
	}
	return
}

func get_cf_credentials() (res string, err error) {
	ini_path, has := os.LookupEnv("CLOUDFLARE_INI")
	if !has {
		err = fmt.Errorf("Need \"CLOUDFLARE_INI\" environment variable.")
		return
	}
	read, err := ioutil.ReadFile(ini_path)
	if err != nil {
		return
	}
	str := string(read)
	lines := strings.Split(str, "\n")
	EINVALID := fmt.Errorf("Invalid credential file")
	for _, l := range lines {
		if l == "" {
			continue
		}
		comp := strings.SplitN(l, " = ", 2)
		if len(comp) != 2 {
			err = EINVALID
			return
		}
		if strings.TrimSpace(comp[0]) == "dns_cloudflare_api_token" {
			res = strings.TrimSpace(comp[1])
		}
	}
	if res == "" {
		err = EINVALID
		return
	}
	return
}

func update(dry_run bool) (err error) {
	if dry_run {
		fmt.Printf("Doing dry-run, no update will be applied.\n")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m, err := get_all_machines_ip(ctx)
	if err != nil {
		return
	}
	cf_token, err := get_cf_credentials()
	if err != nil {
		return
	}
	cf, err := cloudflare.NewWithAPIToken(cf_token, cloudflare.UserAgent(USER_AGENT))
	if err != nil {
		return
	}
	zones, err := cf.ListZones()
	if err != nil {
		err = fmt.Errorf("Unable to list zones: %w", err)
		return
	}
	for domain, ip := range m {
		domain = strings.TrimRight(domain, ".")
		var found_zone *cloudflare.Zone = nil
		for _, z := range zones {
			if strings.HasSuffix(domain, z.Name) {
				found_zone = &z
				break
			}
		}
		if found_zone == nil {
			continue
		}
		var existing_recs []cloudflare.DNSRecord
		existing_recs, err = cf.DNSRecords(found_zone.ID, cloudflare.DNSRecord{Type: "A", Name: domain})
		if err != nil {
			err = fmt.Errorf("Unable to get existing record: %w", err)
			return
		}
		if len(existing_recs) > 1 {
			err = fmt.Errorf("Expected %s to have either 0 or 1 records, got %d.", domain, len(existing_recs))
			return
		}
		existing_ip := ""
		if len(existing_recs) > 0 {
			existing_ip = existing_recs[0].Content
			if existing_ip == "" {
				err = fmt.Errorf("Invalid existing record with empty content on %s", domain)
				return
			}
		}
		if ip == existing_ip {
			fmt.Printf("%s: unchanged\n", domain)
			continue
		}
		if ip != "" {
			new_rec := cloudflare.DNSRecord{
				Type:    "A",
				Name:    domain,
				Content: ip,
				Proxied: false,
			}
			if existing_ip != "" {
				if !dry_run {
					err = cf.UpdateDNSRecord(found_zone.ID, existing_recs[0].ID, new_rec)
					if err != nil {
						err = fmt.Errorf("Failed to update %s: %w", domain, err)
						return
					}
				}
				fmt.Printf("%s updated from %s to %s.\n", domain, existing_ip, ip)
			} else {
				if !dry_run {
					_, err = cf.CreateDNSRecord(found_zone.ID, new_rec)
					if err != nil {
						err = fmt.Errorf("Failed to create %s: %w", domain, err)
						return
					}
				}
				fmt.Printf("%s created and set to %s\n", domain, ip)
			}
		} else {
			if !dry_run {
				err = cf.DeleteDNSRecord(found_zone.ID, existing_recs[0].ID)
				if err != nil {
					err = fmt.Errorf("Failed to delete %s: %w", domain, err)
					return
				}
			}
			fmt.Printf("%s removed (was %s).\n", domain, existing_recs[0].Content)
		}
	}
	return
}

func main() {
	err := update(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to update: %s\n", err.Error())
	}
}
