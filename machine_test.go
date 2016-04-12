// Copyright 2016 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package gomaasapi

import (
	"encoding/base64"
	"net/http"

	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"
)

type machineSuite struct {
	testing.LoggingCleanupSuite
}

var _ = gc.Suite(&machineSuite{})

func (*machineSuite) TestReadMachinesBadSchema(c *gc.C) {
	_, err := readMachines(twoDotOh, "wat?")
	c.Check(err, jc.Satisfies, IsDeserializationError)
	c.Assert(err.Error(), gc.Equals, `machine base schema check failed: expected list, got string("wat?")`)

	_, err = readMachines(twoDotOh, []map[string]interface{}{
		{
			"wat": "?",
		},
	})
	c.Check(err, jc.Satisfies, IsDeserializationError)
	c.Assert(err, gc.ErrorMatches, `machine 0: machine 2.0 schema check failed: .*`)
}

func (*machineSuite) TestReadMachines(c *gc.C) {
	machines, err := readMachines(twoDotOh, parseJSON(c, machinesResponse))
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(machines, gc.HasLen, 3)

	machine := machines[0]

	c.Check(machine.SystemID(), gc.Equals, "4y3ha3")
	c.Check(machine.Hostname(), gc.Equals, "untasted-markita")
	c.Check(machine.FQDN(), gc.Equals, "untasted-markita.maas")
	c.Check(machine.Tags(), jc.DeepEquals, []string{"virtual", "magic"})

	c.Check(machine.IPAddresses(), jc.DeepEquals, []string{"192.168.100.4"})
	c.Check(machine.Memory(), gc.Equals, 1024)
	c.Check(machine.CPUCount(), gc.Equals, 1)
	c.Check(machine.PowerState(), gc.Equals, "on")
	c.Check(machine.Zone().Name(), gc.Equals, "default")
	c.Check(machine.OperatingSystem(), gc.Equals, "ubuntu")
	c.Check(machine.DistroSeries(), gc.Equals, "trusty")
	c.Check(machine.Architecture(), gc.Equals, "amd64/generic")
	c.Check(machine.StatusName(), gc.Equals, "Deployed")
	c.Check(machine.StatusMessage(), gc.Equals, "From 'Deploying' to 'Deployed'")

	bootInterface := machine.BootInterface()
	c.Assert(bootInterface, gc.NotNil)
	c.Check(bootInterface.Name(), gc.Equals, "eth0")

	interfaceSet := machine.InterfaceSet()
	c.Assert(interfaceSet, gc.HasLen, 1)
	id := interfaceSet[0].ID()
	c.Assert(machine.Interface(id), jc.DeepEquals, interfaceSet[0])
	c.Assert(machine.Interface(id+5), gc.IsNil)
}

func (*machineSuite) TestLowVersion(c *gc.C) {
	_, err := readMachines(version.MustParse("1.9.0"), parseJSON(c, machinesResponse))
	c.Assert(err, jc.Satisfies, IsUnsupportedVersionError)
	c.Assert(err.Error(), gc.Equals, `no machine read func for version 1.9.0`)
}

func (*machineSuite) TestHighVersion(c *gc.C) {
	machines, err := readMachines(version.MustParse("2.1.9"), parseJSON(c, machinesResponse))
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(machines, gc.HasLen, 3)
}

func (s *machineSuite) getServerAndMachine(c *gc.C) (*SimpleTestServer, *machine) {
	server, controller := createTestServerController(c, s)
	// Just have machines return one machine
	server.AddGetResponse("/api/2.0/machines/", http.StatusOK, "["+machineResponse+"]")
	machines, err := controller.Machines(MachinesArgs{})
	c.Assert(err, jc.ErrorIsNil)
	c.Check(machines, gc.HasLen, 1)
	machine := machines[0].(*machine)
	return server, machine
}

func (s *machineSuite) TestStart(c *gc.C) {
	server, machine := s.getServerAndMachine(c)
	response := updateJSONMap(c, machineResponse, map[string]interface{}{
		"status_name":    "Deploying",
		"status_message": "for testing",
	})
	server.AddPostResponse(machine.resourceURI+"?op=deploy", http.StatusOK, response)

	err := machine.Start(StartArgs{
		UserData:     []byte("userdata"),
		DistroSeries: "trusty",
		Kernel:       "kernel",
		Comment:      "a comment",
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(machine.StatusName(), gc.Equals, "Deploying")
	c.Assert(machine.StatusMessage(), gc.Equals, "for testing")

	request := server.LastRequest()
	// There should be one entry in the form values for each of the args.
	c.Assert(request.PostForm, gc.HasLen, 4)
	// The userdata should be base64 encoded.
	userdata := request.PostForm.Get("user_data")
	value, err := base64.StdEncoding.DecodeString(userdata)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(value), gc.Equals, "userdata")
}

func (s *machineSuite) TestStartMachineNotFound(c *gc.C) {
	server, machine := s.getServerAndMachine(c)
	server.AddPostResponse(machine.resourceURI+"?op=deploy", http.StatusNotFound, "can't find machine")
	err := machine.Start(StartArgs{})
	c.Assert(err, jc.Satisfies, IsBadRequestError)
	c.Assert(err.Error(), gc.Equals, "can't find machine")
}

func (s *machineSuite) TestStartMachineConflict(c *gc.C) {
	server, machine := s.getServerAndMachine(c)
	server.AddPostResponse(machine.resourceURI+"?op=deploy", http.StatusConflict, "machine not allocated")
	err := machine.Start(StartArgs{})
	c.Assert(err, jc.Satisfies, IsBadRequestError)
	c.Assert(err.Error(), gc.Equals, "machine not allocated")
}

func (s *machineSuite) TestStartMachineForbidden(c *gc.C) {
	server, machine := s.getServerAndMachine(c)
	server.AddPostResponse(machine.resourceURI+"?op=deploy", http.StatusForbidden, "machine not yours")
	err := machine.Start(StartArgs{})
	c.Assert(err, jc.Satisfies, IsPermissionError)
	c.Assert(err.Error(), gc.Equals, "machine not yours")
}

func (s *machineSuite) TestStartMachineServiceUnavailable(c *gc.C) {
	server, machine := s.getServerAndMachine(c)
	server.AddPostResponse(machine.resourceURI+"?op=deploy", http.StatusServiceUnavailable, "no ip addresses available")
	err := machine.Start(StartArgs{})
	c.Assert(err, jc.Satisfies, IsCannotCompleteError)
	c.Assert(err.Error(), gc.Equals, "no ip addresses available")
}

func (s *machineSuite) TestStartMachineUnknown(c *gc.C) {
	server, machine := s.getServerAndMachine(c)
	server.AddPostResponse(machine.resourceURI+"?op=deploy", http.StatusMethodNotAllowed, "wat?")
	err := machine.Start(StartArgs{})
	c.Assert(err, jc.Satisfies, IsUnexpectedError)
	c.Assert(err.Error(), gc.Equals, "unexpected: ServerError: 405 Method Not Allowed (wat?)")
}

const (
	machineResponse = `
	{
        "netboot": false,
        "system_id": "4y3ha3",
        "ip_addresses": [
            "192.168.100.4"
        ],
        "virtualblockdevice_set": [],
        "memory": 1024,
        "cpu_count": 1,
        "hwe_kernel": "hwe-t",
        "status_action": "",
        "osystem": "ubuntu",
        "node_type_name": "Machine",
        "macaddress_set": [
            {
                "mac_address": "52:54:00:55:b6:80"
            }
        ],
        "special_filesystems": [],
        "status": 6,
        "physicalblockdevice_set": [
            {
                "path": "/dev/disk/by-dname/sda",
                "name": "sda",
                "used_for": "MBR partitioned with 1 partition",
                "partitions": [
                    {
                        "bootable": false,
                        "id": 1,
                        "path": "/dev/disk/by-dname/sda-part1",
                        "filesystem": {
                            "fstype": "ext4",
                            "mount_point": "/",
                            "label": "root",
                            "mount_options": null,
                            "uuid": "fcd7745e-f1b5-4f5d-9575-9b0bb796b752"
                        },
                        "type": "partition",
                        "resource_uri": "/MAAS/api/2.0/nodes/4y3ha3/blockdevices/34/partition/1",
                        "uuid": "6199b7c9-b66f-40f6-a238-a938a58a0adf",
                        "used_for": "ext4 formatted filesystem mounted at /",
                        "size": 8581545984
                    }
                ],
                "filesystem": null,
                "id_path": "/dev/disk/by-id/ata-QEMU_HARDDISK_QM00001",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha3/blockdevices/34/",
                "id": 34,
                "serial": "QM00001",
                "type": "physical",
                "block_size": 4096,
                "used_size": 8586788864,
                "available_size": 0,
                "partition_table_type": "MBR",
                "uuid": null,
                "size": 8589934592,
                "model": "QEMU HARDDISK",
                "tags": [
                    "rotary"
                ]
            }
        ],
        "interface_set": [
            {
                "effective_mtu": 1500,
                "mac_address": "52:54:00:55:b6:80",
                "children": [],
                "discovered": [],
                "params": "",
                "vlan": {
                    "resource_uri": "/MAAS/api/2.0/vlans/1/",
                    "id": 1,
                    "secondary_rack": null,
                    "mtu": 1500,
                    "primary_rack": "4y3h7n",
                    "name": "untagged",
                    "fabric": "fabric-0",
                    "dhcp_on": true,
                    "vid": 0
                },
                "name": "eth0",
                "enabled": true,
                "parents": [],
                "id": 35,
                "type": "physical",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha3/interfaces/35/",
                "tags": [],
                "links": [
                    {
                        "id": 82,
                        "ip_address": "192.168.100.4",
                        "subnet": {
                            "resource_uri": "/MAAS/api/2.0/subnets/1/",
                            "id": 1,
                            "rdns_mode": 2,
                            "vlan": {
                                "resource_uri": "/MAAS/api/2.0/vlans/1/",
                                "id": 1,
                                "secondary_rack": null,
                                "mtu": 1500,
                                "primary_rack": "4y3h7n",
                                "name": "untagged",
                                "fabric": "fabric-0",
                                "dhcp_on": true,
                                "vid": 0
                            },
                            "dns_servers": [],
                            "space": "space-0",
                            "name": "192.168.100.0/24",
                            "gateway_ip": "192.168.100.1",
                            "cidr": "192.168.100.0/24"
                        },
                        "mode": "auto"
                    }
                ]
            }
        ],
        "resource_uri": "/MAAS/api/2.0/machines/4y3ha3/",
        "hostname": "untasted-markita",
        "status_name": "Deployed",
        "min_hwe_kernel": "",
        "address_ttl": null,
        "boot_interface": {
            "effective_mtu": 1500,
            "mac_address": "52:54:00:55:b6:80",
            "children": [],
            "discovered": [],
            "params": "",
            "vlan": {
                "resource_uri": "/MAAS/api/2.0/vlans/1/",
                "id": 1,
                "secondary_rack": null,
                "mtu": 1500,
                "primary_rack": "4y3h7n",
                "name": "untagged",
                "fabric": "fabric-0",
                "dhcp_on": true,
                "vid": 0
            },
            "name": "eth0",
            "enabled": true,
            "parents": [],
            "id": 35,
            "type": "physical",
            "resource_uri": "/MAAS/api/2.0/nodes/4y3ha3/interfaces/35/",
            "tags": [],
            "links": [
                {
                    "id": 82,
                    "ip_address": "192.168.100.4",
                    "subnet": {
                        "resource_uri": "/MAAS/api/2.0/subnets/1/",
                        "id": 1,
                        "rdns_mode": 2,
                        "vlan": {
                            "resource_uri": "/MAAS/api/2.0/vlans/1/",
                            "id": 1,
                            "secondary_rack": null,
                            "mtu": 1500,
                            "primary_rack": "4y3h7n",
                            "name": "untagged",
                            "fabric": "fabric-0",
                            "dhcp_on": true,
                            "vid": 0
                        },
                        "dns_servers": [],
                        "space": "space-0",
                        "name": "192.168.100.0/24",
                        "gateway_ip": "192.168.100.1",
                        "cidr": "192.168.100.0/24"
                    },
                    "mode": "auto"
                }
            ]
        },
        "power_state": "on",
        "architecture": "amd64/generic",
        "power_type": "virsh",
        "distro_series": "trusty",
        "tag_names": [
            "virtual", "magic"
        ],
        "disable_ipv4": false,
        "status_message": "From 'Deploying' to 'Deployed'",
        "swap_size": null,
        "blockdevice_set": [
            {
                "path": "/dev/disk/by-dname/sda",
                "partition_table_type": "MBR",
                "name": "sda",
                "used_for": "MBR partitioned with 1 partition",
                "partitions": [
                    {
                        "bootable": false,
                        "id": 1,
                        "path": "/dev/disk/by-dname/sda-part1",
                        "filesystem": {
                            "fstype": "ext4",
                            "mount_point": "/",
                            "label": "root",
                            "mount_options": null,
                            "uuid": "fcd7745e-f1b5-4f5d-9575-9b0bb796b752"
                        },
                        "type": "partition",
                        "resource_uri": "/MAAS/api/2.0/nodes/4y3ha3/blockdevices/34/partition/1",
                        "uuid": "6199b7c9-b66f-40f6-a238-a938a58a0adf",
                        "used_for": "ext4 formatted filesystem mounted at /",
                        "size": 8581545984
                    }
                ],
                "filesystem": null,
                "id_path": "/dev/disk/by-id/ata-QEMU_HARDDISK_QM00001",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha3/blockdevices/34/",
                "id": 34,
                "serial": "QM00001",
                "block_size": 4096,
                "type": "physical",
                "used_size": 8586788864,
                "tags": [
                    "rotary"
                ],
                "available_size": 0,
                "uuid": null,
                "size": 8589934592,
                "model": "QEMU HARDDISK"
            }
        ],
        "zone": {
            "description": "",
            "resource_uri": "/MAAS/api/2.0/zones/default/",
            "name": "default"
        },
        "fqdn": "untasted-markita.maas",
        "storage": 8589.934592,
        "node_type": 0,
        "boot_disk": null,
        "owner": "thumper",
        "domain": {
            "id": 0,
            "name": "maas",
            "resource_uri": "/MAAS/api/2.0/domains/0/",
            "resource_record_count": 0,
            "ttl": null,
            "authoritative": true
        }
    }
`
	machinesResponse = "[" + machineResponse + `,
    {
        "netboot": true,
        "system_id": "4y3ha4",
        "ip_addresses": [],
        "virtualblockdevice_set": [],
        "memory": 1024,
        "cpu_count": 1,
        "hwe_kernel": "",
        "status_action": "",
        "osystem": "",
        "node_type_name": "Machine",
        "macaddress_set": [
            {
                "mac_address": "52:54:00:33:6b:2c"
            }
        ],
        "special_filesystems": [],
        "status": 4,
        "physicalblockdevice_set": [
            {
                "path": "/dev/disk/by-dname/sda",
                "name": "sda",
                "used_for": "MBR partitioned with 1 partition",
                "partitions": [
                    {
                        "bootable": false,
                        "id": 2,
                        "path": "/dev/disk/by-dname/sda-part1",
                        "filesystem": {
                            "fstype": "ext4",
                            "mount_point": "/",
                            "label": "root",
                            "mount_options": null,
                            "uuid": "7a0e75a8-0bc6-456b-ac92-4769e97baf02"
                        },
                        "type": "partition",
                        "resource_uri": "/MAAS/api/2.0/nodes/4y3ha4/blockdevices/35/partition/2",
                        "uuid": "6fe782cf-ad1a-4b31-8beb-333401b4d4bb",
                        "used_for": "ext4 formatted filesystem mounted at /",
                        "size": 8581545984
                    }
                ],
                "filesystem": null,
                "id_path": "/dev/disk/by-id/ata-QEMU_HARDDISK_QM00001",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha4/blockdevices/35/",
                "id": 35,
                "serial": "QM00001",
                "type": "physical",
                "block_size": 4096,
                "used_size": 8586788864,
                "available_size": 0,
                "partition_table_type": "MBR",
                "uuid": null,
                "size": 8589934592,
                "model": "QEMU HARDDISK",
                "tags": [
                    "rotary"
                ]
            }
        ],
        "interface_set": [
            {
                "effective_mtu": 1500,
                "mac_address": "52:54:00:33:6b:2c",
                "children": [],
                "discovered": [],
                "params": "",
                "vlan": {
                    "resource_uri": "/MAAS/api/2.0/vlans/1/",
                    "id": 1,
                    "secondary_rack": null,
                    "mtu": 1500,
                    "primary_rack": "4y3h7n",
                    "name": "untagged",
                    "fabric": "fabric-0",
                    "dhcp_on": true,
                    "vid": 0
                },
                "name": "eth0",
                "enabled": true,
                "parents": [],
                "id": 39,
                "type": "physical",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha4/interfaces/39/",
                "tags": [],
                "links": [
                    {
                        "id": 67,
                        "mode": "auto",
                        "subnet": {
                            "resource_uri": "/MAAS/api/2.0/subnets/1/",
                            "id": 1,
                            "rdns_mode": 2,
                            "vlan": {
                                "resource_uri": "/MAAS/api/2.0/vlans/1/",
                                "id": 1,
                                "secondary_rack": null,
                                "mtu": 1500,
                                "primary_rack": "4y3h7n",
                                "name": "untagged",
                                "fabric": "fabric-0",
                                "dhcp_on": true,
                                "vid": 0
                            },
                            "dns_servers": [],
                            "space": "space-0",
                            "name": "192.168.100.0/24",
                            "gateway_ip": "192.168.100.1",
                            "cidr": "192.168.100.0/24"
                        }
                    }
                ]
            }
        ],
        "resource_uri": "/MAAS/api/2.0/machines/4y3ha4/",
        "hostname": "lowlier-glady",
        "status_name": "Ready",
        "min_hwe_kernel": "",
        "address_ttl": null,
        "boot_interface": {
            "effective_mtu": 1500,
            "mac_address": "52:54:00:33:6b:2c",
            "children": [],
            "discovered": [],
            "params": "",
            "vlan": {
                "resource_uri": "/MAAS/api/2.0/vlans/1/",
                "id": 1,
                "secondary_rack": null,
                "mtu": 1500,
                "primary_rack": "4y3h7n",
                "name": "untagged",
                "fabric": "fabric-0",
                "dhcp_on": true,
                "vid": 0
            },
            "name": "eth0",
            "enabled": true,
            "parents": [],
            "id": 39,
            "type": "physical",
            "resource_uri": "/MAAS/api/2.0/nodes/4y3ha4/interfaces/39/",
            "tags": [],
            "links": [
                {
                    "id": 67,
                    "mode": "auto",
                    "subnet": {
                        "resource_uri": "/MAAS/api/2.0/subnets/1/",
                        "id": 1,
                        "rdns_mode": 2,
                        "vlan": {
                            "resource_uri": "/MAAS/api/2.0/vlans/1/",
                            "id": 1,
                            "secondary_rack": null,
                            "mtu": 1500,
                            "primary_rack": "4y3h7n",
                            "name": "untagged",
                            "fabric": "fabric-0",
                            "dhcp_on": true,
                            "vid": 0
                        },
                        "dns_servers": [],
                        "space": "space-0",
                        "name": "192.168.100.0/24",
                        "gateway_ip": "192.168.100.1",
                        "cidr": "192.168.100.0/24"
                    }
                }
            ]
        },
        "power_state": "off",
        "architecture": "amd64/generic",
        "power_type": "virsh",
        "distro_series": "",
        "tag_names": [
            "virtual"
        ],
        "disable_ipv4": false,
        "status_message": "From 'Commissioning' to 'Ready'",
        "swap_size": null,
        "blockdevice_set": [
            {
                "path": "/dev/disk/by-dname/sda",
                "partition_table_type": "MBR",
                "name": "sda",
                "used_for": "MBR partitioned with 1 partition",
                "partitions": [
                    {
                        "bootable": false,
                        "id": 2,
                        "path": "/dev/disk/by-dname/sda-part1",
                        "filesystem": {
                            "fstype": "ext4",
                            "mount_point": "/",
                            "label": "root",
                            "mount_options": null,
                            "uuid": "7a0e75a8-0bc6-456b-ac92-4769e97baf02"
                        },
                        "type": "partition",
                        "resource_uri": "/MAAS/api/2.0/nodes/4y3ha4/blockdevices/35/partition/2",
                        "uuid": "6fe782cf-ad1a-4b31-8beb-333401b4d4bb",
                        "used_for": "ext4 formatted filesystem mounted at /",
                        "size": 8581545984
                    }
                ],
                "filesystem": null,
                "id_path": "/dev/disk/by-id/ata-QEMU_HARDDISK_QM00001",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha4/blockdevices/35/",
                "id": 35,
                "serial": "QM00001",
                "block_size": 4096,
                "type": "physical",
                "used_size": 8586788864,
                "tags": [
                    "rotary"
                ],
                "available_size": 0,
                "uuid": null,
                "size": 8589934592,
                "model": "QEMU HARDDISK"
            }
        ],
        "zone": {
            "description": "",
            "resource_uri": "/MAAS/api/2.0/zones/default/",
            "name": "default"
        },
        "fqdn": "lowlier-glady.maas",
        "storage": 8589.934592,
        "node_type": 0,
        "boot_disk": null,
        "owner": null,
        "domain": {
            "id": 0,
            "name": "maas",
            "resource_uri": "/MAAS/api/2.0/domains/0/",
            "resource_record_count": 0,
            "ttl": null,
            "authoritative": true
        }
    },
    {
        "netboot": true,
        "system_id": "4y3ha6",
        "ip_addresses": [],
        "virtualblockdevice_set": [],
        "memory": 1024,
        "cpu_count": 1,
        "hwe_kernel": "",
        "status_action": "",
        "osystem": "",
        "node_type_name": "Machine",
        "macaddress_set": [
            {
                "mac_address": "52:54:00:c9:6a:45"
            }
        ],
        "special_filesystems": [],
        "status": 4,
        "physicalblockdevice_set": [
            {
                "path": "/dev/disk/by-dname/sda",
                "name": "sda",
                "used_for": "MBR partitioned with 1 partition",
                "partitions": [
                    {
                        "bootable": false,
                        "id": 3,
                        "path": "/dev/disk/by-dname/sda-part1",
                        "filesystem": {
                            "fstype": "ext4",
                            "mount_point": "/",
                            "label": "root",
                            "mount_options": null,
                            "uuid": "f15b4e94-7dc3-460d-8838-0c299905c799"
                        },
                        "type": "partition",
                        "resource_uri": "/MAAS/api/2.0/nodes/4y3ha6/blockdevices/36/partition/3",
                        "uuid": "a20ae130-bd8f-41b5-bdb3-47ab11a621b5",
                        "used_for": "ext4 formatted filesystem mounted at /",
                        "size": 8581545984
                    }
                ],
                "filesystem": null,
                "id_path": "/dev/disk/by-id/ata-QEMU_HARDDISK_QM00001",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha6/blockdevices/36/",
                "id": 36,
                "serial": "QM00001",
                "type": "physical",
                "block_size": 4096,
                "used_size": 8586788864,
                "available_size": 0,
                "partition_table_type": "MBR",
                "uuid": null,
                "size": 8589934592,
                "model": "QEMU HARDDISK",
                "tags": [
                    "rotary"
                ]
            }
        ],
        "interface_set": [
            {
                "effective_mtu": 1500,
                "mac_address": "52:54:00:c9:6a:45",
                "children": [],
                "discovered": [],
                "params": "",
                "vlan": {
                    "resource_uri": "/MAAS/api/2.0/vlans/1/",
                    "id": 1,
                    "secondary_rack": null,
                    "mtu": 1500,
                    "primary_rack": "4y3h7n",
                    "name": "untagged",
                    "fabric": "fabric-0",
                    "dhcp_on": true,
                    "vid": 0
                },
                "name": "eth0",
                "enabled": true,
                "parents": [],
                "id": 40,
                "type": "physical",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha6/interfaces/40/",
                "tags": [],
                "links": [
                    {
                        "id": 69,
                        "mode": "auto",
                        "subnet": {
                            "resource_uri": "/MAAS/api/2.0/subnets/1/",
                            "id": 1,
                            "rdns_mode": 2,
                            "vlan": {
                                "resource_uri": "/MAAS/api/2.0/vlans/1/",
                                "id": 1,
                                "secondary_rack": null,
                                "mtu": 1500,
                                "primary_rack": "4y3h7n",
                                "name": "untagged",
                                "fabric": "fabric-0",
                                "dhcp_on": true,
                                "vid": 0
                            },
                            "dns_servers": [],
                            "space": "space-0",
                            "name": "192.168.100.0/24",
                            "gateway_ip": "192.168.100.1",
                            "cidr": "192.168.100.0/24"
                        }
                    }
                ]
            }
        ],
        "resource_uri": "/MAAS/api/2.0/machines/4y3ha6/",
        "hostname": "icier-nina",
        "status_name": "Ready",
        "min_hwe_kernel": "",
        "address_ttl": null,
        "boot_interface": {
            "effective_mtu": 1500,
            "mac_address": "52:54:00:c9:6a:45",
            "children": [],
            "discovered": [],
            "params": "",
            "vlan": {
                "resource_uri": "/MAAS/api/2.0/vlans/1/",
                "id": 1,
                "secondary_rack": null,
                "mtu": 1500,
                "primary_rack": "4y3h7n",
                "name": "untagged",
                "fabric": "fabric-0",
                "dhcp_on": true,
                "vid": 0
            },
            "name": "eth0",
            "enabled": true,
            "parents": [],
            "id": 40,
            "type": "physical",
            "resource_uri": "/MAAS/api/2.0/nodes/4y3ha6/interfaces/40/",
            "tags": [],
            "links": [
                {
                    "id": 69,
                    "mode": "auto",
                    "subnet": {
                        "resource_uri": "/MAAS/api/2.0/subnets/1/",
                        "id": 1,
                        "rdns_mode": 2,
                        "vlan": {
                            "resource_uri": "/MAAS/api/2.0/vlans/1/",
                            "id": 1,
                            "secondary_rack": null,
                            "mtu": 1500,
                            "primary_rack": "4y3h7n",
                            "name": "untagged",
                            "fabric": "fabric-0",
                            "dhcp_on": true,
                            "vid": 0
                        },
                        "dns_servers": [],
                        "space": "space-0",
                        "name": "192.168.100.0/24",
                        "gateway_ip": "192.168.100.1",
                        "cidr": "192.168.100.0/24"
                    }
                }
            ]
        },
        "power_state": "off",
        "architecture": "amd64/generic",
        "power_type": "virsh",
        "distro_series": "",
        "tag_names": [
            "virtual"
        ],
        "disable_ipv4": false,
        "status_message": "From 'Commissioning' to 'Ready'",
        "swap_size": null,
        "blockdevice_set": [
            {
                "path": "/dev/disk/by-dname/sda",
                "partition_table_type": "MBR",
                "name": "sda",
                "used_for": "MBR partitioned with 1 partition",
                "partitions": [
                    {
                        "bootable": false,
                        "id": 3,
                        "path": "/dev/disk/by-dname/sda-part1",
                        "filesystem": {
                            "fstype": "ext4",
                            "mount_point": "/",
                            "label": "root",
                            "mount_options": null,
                            "uuid": "f15b4e94-7dc3-460d-8838-0c299905c799"
                        },
                        "type": "partition",
                        "resource_uri": "/MAAS/api/2.0/nodes/4y3ha6/blockdevices/36/partition/3",
                        "uuid": "a20ae130-bd8f-41b5-bdb3-47ab11a621b5",
                        "used_for": "ext4 formatted filesystem mounted at /",
                        "size": 8581545984
                    }
                ],
                "filesystem": null,
                "id_path": "/dev/disk/by-id/ata-QEMU_HARDDISK_QM00001",
                "resource_uri": "/MAAS/api/2.0/nodes/4y3ha6/blockdevices/36/",
                "id": 36,
                "serial": "QM00001",
                "block_size": 4096,
                "type": "physical",
                "used_size": 8586788864,
                "tags": [
                    "rotary"
                ],
                "available_size": 0,
                "uuid": null,
                "size": 8589934592,
                "model": "QEMU HARDDISK"
            }
        ],
        "zone": {
            "description": "",
            "resource_uri": "/MAAS/api/2.0/zones/default/",
            "name": "default"
        },
        "fqdn": "icier-nina.maas",
        "storage": 8589.934592,
        "node_type": 0,
        "boot_disk": null,
        "owner": null,
        "domain": {
            "id": 0,
            "name": "maas",
            "resource_uri": "/MAAS/api/2.0/domains/0/",
            "resource_record_count": 0,
            "ttl": null,
            "authoritative": true
        }
    }
]
`
)