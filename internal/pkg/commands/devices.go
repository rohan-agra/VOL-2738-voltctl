/*
 * Copyright 2019-present Ciena Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fullstorydev/grpcurl"
	flags "github.com/jessevdk/go-flags"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/opencord/voltctl/pkg/format"
	"github.com/opencord/voltctl/pkg/model"
)

const (
	DEFAULT_DEVICE_FORMAT         = "table{{ .Id }}\t{{.Type}}\t{{.Root}}\t{{.ParentId}}\t{{.SerialNumber}}\t{{.AdminState}}\t{{.OperStatus}}\t{{.ConnectStatus}}\t{{.Reason}}"
	DEFAULT_DEVICE_PORTS_FORMAT   = "table{{.PortNo}}\t{{.Label}}\t{{.Type}}\t{{.AdminState}}\t{{.OperStatus}}\t{{.DeviceId}}\t{{.Peers}}"
	DEFAULT_DEVICE_INSPECT_FORMAT = `ID: {{.Id}}
  TYPE:          {{.Type}}
  ROOT:          {{.Root}}
  PARENTID:      {{.ParentId}}
  SERIALNUMBER:  {{.SerialNumber}}
  VLAN:          {{.Vlan}}
  ADMINSTATE:    {{.AdminState}}
  OPERSTATUS:    {{.OperStatus}}
  CONNECTSTATUS: {{.ConnectStatus}}`
)

type DeviceList struct {
	ListOutputOptions
}

type DeviceCreate struct {
	DeviceType  string `short:"t" long:"devicetype" default:"" description:"Device type"`
	MACAddress  string `short:"m" long:"macaddress" default:"" description:"MAC Address"`
	IPAddress   string `short:"i" long:"ipaddress" default:"" description:"IP Address"`
	HostAndPort string `short:"H" long:"hostandport" default:"" description:"Host and port"`
}

type DeviceId string

type PortNum uint32

type DeviceDelete struct {
	Args struct {
		Ids []DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DeviceEnable struct {
	Args struct {
		Ids []DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DeviceDisable struct {
	Args struct {
		Ids []DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DeviceReboot struct {
	Args struct {
		Ids []DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DeviceFlowList struct {
	ListOutputOptions
	Args struct {
		Id DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DevicePortList struct {
	ListOutputOptions
	Args struct {
		Id DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DeviceInspect struct {
	OutputOptionsJson
	Args struct {
		Id DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
	} `positional-args:"yes"`
}

type DevicePortEnable struct {
	Args struct {
		Id     DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
		PortId PortNum  `positional-arg-name:"PORT_NUMBER" required:"yes"`
	} `positional-args:"yes"`
}

type DevicePortDisable struct {
	Args struct {
		Id     DeviceId `positional-arg-name:"DEVICE_ID" required:"yes"`
		PortId PortNum  `positional-arg-name:"PORT_NUMBER" required:"yes"`
	} `positional-args:"yes"`
}

type DeviceOpts struct {
	List    DeviceList     `command:"list"`
	Create  DeviceCreate   `command:"create"`
	Delete  DeviceDelete   `command:"delete"`
	Enable  DeviceEnable   `command:"enable"`
	Disable DeviceDisable  `command:"disable"`
	Flows   DeviceFlowList `command:"flows"`
	Port    struct {
		List    DevicePortList    `command:"list"`
		Enable  DevicePortEnable  `command:"enable"`
		Disable DevicePortDisable `command:"disable"`
	} `command:"port"`
	Inspect DeviceInspect `command:"inspect"`
	Reboot  DeviceReboot  `command:"reboot"`
}

var deviceOpts = DeviceOpts{}

func RegisterDeviceCommands(parser *flags.Parser) {
	if _, err := parser.AddCommand("device", "device commands", "Commands to query and manipulate VOLTHA devices", &deviceOpts); err != nil {
		Error.Fatalf("Unexpected error while attempting to register device commands : %s", err)
	}
}

func (i *PortNum) Complete(match string) []flags.Completion {
	conn, err := NewConnection()
	if err != nil {
		return nil
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-ports")
	if err != nil {
		return nil
	}

	/*
	 * The command line args when completing for PortNum will be a DeviceId
	 * followed by one or more PortNums. So walk the argument list from the
	 * end and find the first argument that is enable/disable as those are
	 * the subcommands that come before the positional arguments. It would
	 * be nice if this package gave us the list of optional arguments
	 * already parsed.
	 */
	var deviceId string
found:
	for i := len(os.Args) - 1; i >= 0; i -= 1 {
		switch os.Args[i] {
		case "enable":
			fallthrough
		case "disable":
			if len(os.Args) > i+1 {
				deviceId = os.Args[i+1]
			} else {
				return nil
			}
			break found
		default:
		}
	}

	if len(deviceId) == 0 {
		return nil
	}

	h := &RpcEventHandler{
		Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": deviceId}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()
	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		return nil
	}

	if h.Status != nil && h.Status.Err() != nil {
		return nil
	}

	d, err := dynamic.AsDynamicMessage(h.Response)
	if err != nil {
		return nil
	}

	items, err := d.TryGetFieldByName("items")
	if err != nil {
		return nil
	}

	list := make([]flags.Completion, 0)
	for _, item := range items.([]interface{}) {
		val := item.(*dynamic.Message)
		pn := strconv.FormatUint(uint64(val.GetFieldByName("port_no").(uint32)), 10)
		if strings.HasPrefix(pn, match) {
			list = append(list, flags.Completion{Item: pn})
		}
	}

	return list
}

func (i *DeviceId) Complete(match string) []flags.Completion {
	conn, err := NewConnection()
	if err != nil {
		return nil
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-list")
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	h := &RpcEventHandler{}
	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		return nil
	}

	if h.Status != nil && h.Status.Err() != nil {
		return nil
	}

	d, err := dynamic.AsDynamicMessage(h.Response)
	if err != nil {
		return nil
	}

	items, err := d.TryGetFieldByName("items")
	if err != nil {
		return nil
	}

	list := make([]flags.Completion, 0)
	for _, item := range items.([]interface{}) {
		val := item.(*dynamic.Message)
		id := val.GetFieldByName("id").(string)
		if strings.HasPrefix(id, match) {
			list = append(list, flags.Completion{Item: id})
		}
	}

	return list
}

func (options *DeviceList) Execute(args []string) error {

	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-list")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	h := &RpcEventHandler{}
	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		return err
	}

	if h.Status != nil && h.Status.Err() != nil {
		return h.Status.Err()
	}

	d, err := dynamic.AsDynamicMessage(h.Response)
	if err != nil {
		return err
	}

	items, err := d.TryGetFieldByName("items")
	if err != nil {
		return err
	}

	outputFormat := CharReplacer.Replace(options.Format)
	if outputFormat == "" {
		outputFormat = GetCommandOptionWithDefault("device-list", "format", DEFAULT_DEVICE_FORMAT)
	}
	if options.Quiet {
		outputFormat = "{{.Id}}"
	}

	orderBy := options.OrderBy
	if orderBy == "" {
		orderBy = GetCommandOptionWithDefault("device-list", "order", "")
	}

	data := make([]model.Device, len(items.([]interface{})))
	for i, item := range items.([]interface{}) {
		val := item.(*dynamic.Message)
		data[i].PopulateFrom(val)
	}

	result := CommandResult{
		Format:    format.Format(outputFormat),
		Filter:    options.Filter,
		OrderBy:   orderBy,
		OutputAs:  toOutputType(options.OutputAs),
		NameLimit: options.NameLimit,
		Data:      data,
	}

	GenerateOutput(&result)
	return nil
}

func (options *DeviceCreate) Execute(args []string) error {

	dm := make(map[string]interface{})
	if options.HostAndPort != "" {
		dm["host_and_port"] = options.HostAndPort
	} else if options.IPAddress != "" {
		dm["ipv4_address"] = options.IPAddress
	}
	if options.MACAddress != "" {
		dm["mac_address"] = strings.ToLower(options.MACAddress)
	}
	if options.DeviceType != "" {
		dm["type"] = options.DeviceType
	}

	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-create")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	h := &RpcEventHandler{
		Fields: map[string]map[string]interface{}{"voltha.Device": dm},
	}
	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		return err
	} else if h.Status != nil && h.Status.Err() != nil {
		return h.Status.Err()
	}

	resp, err := dynamic.AsDynamicMessage(h.Response)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", resp.GetFieldByName("id").(string))

	return nil
}

func (options *DeviceDelete) Execute(args []string) error {

	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-delete")
	if err != nil {
		return err
	}

	var lastErr error
	for _, i := range options.Args.Ids {

		h := &RpcEventHandler{
			Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": i}},
		}
		ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
		defer cancel()

		err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
		if err != nil {
			Error.Printf("Error while deleting '%s': %s\n", i, err)
			lastErr = err
			continue
		} else if h.Status != nil && h.Status.Err() != nil {
			Error.Printf("Error while deleting '%s': %s\n", i, ErrorToString(h.Status.Err()))
			lastErr = h.Status.Err()
			continue
		}
		fmt.Printf("%s\n", i)
	}

	if lastErr != nil {
		return NoReportErr
	}
	return nil
}

func (options *DeviceEnable) Execute(args []string) error {
	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-enable")
	if err != nil {
		return err
	}

	var lastErr error
	for _, i := range options.Args.Ids {
		h := &RpcEventHandler{
			Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": i}},
		}
		ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
		defer cancel()

		err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
		if err != nil {
			Error.Printf("Error while enabling '%s': %s\n", i, err)
			lastErr = err
			continue
		} else if h.Status != nil && h.Status.Err() != nil {
			Error.Printf("Error while enabling '%s': %s\n", i, ErrorToString(h.Status.Err()))
			lastErr = h.Status.Err()
			continue
		}
		fmt.Printf("%s\n", i)
	}

	if lastErr != nil {
		return NoReportErr
	}
	return nil
}

func (options *DeviceDisable) Execute(args []string) error {
	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-disable")
	if err != nil {
		return err
	}

	var lastErr error
	for _, i := range options.Args.Ids {
		h := &RpcEventHandler{
			Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": i}},
		}
		ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
		defer cancel()

		err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
		if err != nil {
			Error.Printf("Error while disabling '%s': %s\n", i, err)
			lastErr = err
			continue
		} else if h.Status != nil && h.Status.Err() != nil {
			Error.Printf("Error while disabling '%s': %s\n", i, ErrorToString(h.Status.Err()))
			lastErr = h.Status.Err()
			continue
		}
		fmt.Printf("%s\n", i)
	}

	if lastErr != nil {
		return NoReportErr
	}
	return nil
}

func (options *DeviceReboot) Execute(args []string) error {
	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-reboot")
	if err != nil {
		return err
	}

	var lastErr error
	for _, i := range options.Args.Ids {
		h := &RpcEventHandler{
			Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": i}},
		}
		ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
		defer cancel()

		err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
		if err != nil {
			Error.Printf("Error while rebooting '%s': %s\n", i, err)
			lastErr = err
			continue
		} else if h.Status != nil && h.Status.Err() != nil {
			Error.Printf("Error while rebooting '%s': %s\n", i, ErrorToString(h.Status.Err()))
			lastErr = h.Status.Err()
			continue
		}
		fmt.Printf("%s\n", i)
	}

	if lastErr != nil {
		return NoReportErr
	}
	return nil
}

func (options *DevicePortList) Execute(args []string) error {

	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-ports")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	h := &RpcEventHandler{
		Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": options.Args.Id}},
	}
	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		return err
	}

	if h.Status != nil && h.Status.Err() != nil {
		return h.Status.Err()
	}

	d, err := dynamic.AsDynamicMessage(h.Response)
	if err != nil {
		return err
	}

	items, err := d.TryGetFieldByName("items")
	if err != nil {
		return err
	}

	outputFormat := CharReplacer.Replace(options.Format)
	if outputFormat == "" {
		outputFormat = GetCommandOptionWithDefault("device-ports", "format", DEFAULT_DEVICE_PORTS_FORMAT)
	}
	if options.Quiet {
		outputFormat = "{{.Id}}"
	}

	orderBy := options.OrderBy
	if orderBy == "" {
		orderBy = GetCommandOptionWithDefault("device-ports", "order", "")
	}

	data := make([]model.DevicePort, len(items.([]interface{})))
	for i, item := range items.([]interface{}) {
		data[i].PopulateFrom(item.(*dynamic.Message))
	}

	result := CommandResult{
		Format:    format.Format(outputFormat),
		Filter:    options.Filter,
		OrderBy:   orderBy,
		OutputAs:  toOutputType(options.OutputAs),
		NameLimit: options.NameLimit,
		Data:      data,
	}

	GenerateOutput(&result)
	return nil
}

func (options *DeviceFlowList) Execute(args []string) error {
	fl := &FlowList{}
	fl.ListOutputOptions = options.ListOutputOptions
	fl.Args.Id = string(options.Args.Id)
	fl.Method = "device-flows"
	return fl.Execute(args)
}

func (options *DeviceInspect) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("only a single argument 'DEVICE_ID' can be provided")
	}

	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-inspect")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	h := &RpcEventHandler{
		Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["ID"]: {"id": options.Args.Id}},
	}
	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{"Get-Depth: 2"}, h, h.GetParams)
	if err != nil {
		return err
	} else if h.Status != nil && h.Status.Err() != nil {
		return h.Status.Err()
	}

	d, err := dynamic.AsDynamicMessage(h.Response)
	if err != nil {
		return err
	}

	device := &model.Device{}
	device.PopulateFrom(d)

	outputFormat := CharReplacer.Replace(options.Format)
	if outputFormat == "" {
		outputFormat = GetCommandOptionWithDefault("device-inspect", "format", DEFAULT_DEVICE_INSPECT_FORMAT)
	}
	if options.Quiet {
		outputFormat = "{{.Id}}"
	}

	result := CommandResult{
		Format:    format.Format(outputFormat),
		OutputAs:  toOutputType(options.OutputAs),
		NameLimit: options.NameLimit,
		Data:      device,
	}
	GenerateOutput(&result)
	return nil
}

/*Device  Port Enable */
func (options *DevicePortEnable) Execute(args []string) error {
	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-port-enable")
	if err != nil {
		return err
	}

	h := &RpcEventHandler{
		Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["port"]: {"device_id": options.Args.Id, "port_no": options.Args.PortId}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		Error.Printf("Error enabling port number %v on device Id %s,err=%s\n", options.Args.PortId, options.Args.Id, ErrorToString(err))
		return err
	} else if h.Status != nil && h.Status.Err() != nil {
		Error.Printf("Error enabling port number %v on device Id %s,err=%s\n", options.Args.PortId, options.Args.Id, ErrorToString(h.Status.Err()))
		return h.Status.Err()
	}

	return nil
}

/*Device Port Disable */
func (options *DevicePortDisable) Execute(args []string) error {
	conn, err := NewConnection()
	if err != nil {
		return err
	}
	defer conn.Close()

	descriptor, method, err := GetMethod("device-port-disable")
	if err != nil {
		return err
	}
	h := &RpcEventHandler{
		Fields: map[string]map[string]interface{}{ParamNames[GlobalConfig.ApiVersion]["port"]: {"device_id": options.Args.Id, "port_no": options.Args.PortId}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Grpc.Timeout)
	defer cancel()

	err = grpcurl.InvokeRPC(ctx, descriptor, conn, method, []string{}, h, h.GetParams)
	if err != nil {
		Error.Printf("Error disabling port number %v on device Id %s,err=%s\n", options.Args.PortId, options.Args.Id, ErrorToString(err))
		return err
	} else if h.Status != nil && h.Status.Err() != nil {
		Error.Printf("Error disabling port number %v on device Id %s,err=%s\n", options.Args.PortId, options.Args.Id, ErrorToString(h.Status.Err()))
		return h.Status.Err()
	}
	return nil
}
