// Package tool is a tool for using cjdns
package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/inhies/go-cjdns/admin"
	"github.com/inhies/go-cjdns/config"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
)

const (
	Version = "0.2"

	defaultPingTimeout = 10000 //10 seconds
	defaultPingCount   = 0

	defaultLogLevel    = "DEBUG"
	defaultLogFile     = ""
	defaultLogFileLine = 0

	defaultFile = "/etc/cjdroute.conf"

	pingCmd     = "ping"
	logCmd      = "log"
	traceCmd    = "traceroute"
	dumpCmd     = "dump"
	routeCmd    = "route"
	killCmd     = "kill"
	versionsCmd = "versions"

	magicalLinkConstant = 5366870.0
)

var (
	PingTimeout int
	PingCount   int

	LogLevel    string
	LogFile     string
	LogFileLine int

	fs *flag.FlagSet

	File string
)

type Ping struct {
	IP, Version                                  string
	Failed, Percent, Sent, Success               float64
	CTime, TTime, TTime2, TMin, TAvg, TMax, TDev float64
}
type Route struct {
	IP      string
	Path    string
	RawPath string
	RawLink int64
	Link    float64
	Version int64
}

func init() {

	fs = flag.NewFlagSet("cjdcmd", flag.ExitOnError)
	const (
		usagePingTimeout = "[ping] specify the time in milliseconds cjdns should wait for a response"
		usagePingCount   = "[ping] specify the number of packets to send"

		usageLogLevel    = "[log] specify the logging level to use"
		usageLogFile     = "[log] specify the cjdns source file you wish to see log output from"
		usageLogFileLine = "[log] specify the cjdns source file line to log"

		usageFile = "the cjdroute configuration file to use, edit, or view"
	)
	fs.StringVar(&File, "file", defaultFile, usageFile)
	fs.StringVar(&File, "f", defaultFile, usageFile+" (shorthand)")

	fs.IntVar(&PingTimeout, "timeout", defaultPingTimeout, usagePingTimeout)
	fs.IntVar(&PingTimeout, "t", defaultPingTimeout, usagePingTimeout+" (shorthand)")

	fs.IntVar(&PingCount, "count", defaultPingCount, usagePingCount)
	fs.IntVar(&PingCount, "c", defaultPingCount, usagePingCount+" (shorthand)")

	fs.StringVar(&LogLevel, "level", defaultLogLevel, usageLogLevel)
	fs.StringVar(&LogLevel, "l", defaultLogLevel, usageLogLevel+" (shorthand)")

	fs.StringVar(&LogFile, "logfile", defaultLogFile, usageLogFile)
	fs.IntVar(&LogFileLine, "line", defaultLogFileLine, usageLogFileLine)

}
func usage() {
	println("cjdcmd version ", Version)
	println("")
	println("Usage: cjdcmd command [arguments]")
	println("")
	println("The commands are:")
	println("")
	println("ping <ipv6 address or cjdns routing path>  sends a cjdns ping to the specified node")
	println("route <ipv6 address or cjdns routing path> prints out all routes to an IP or the IP to a route")
	println("log [-l level] [-file file] [-line line]   prints cjdns log to stdout")
	println("dump                                       dumps the routing table to stdout")
	println("kill                                       tells cjdns to gracefully exit")
	println("")
	println("Please use `cjdcmd --help` for a list of flags.")
}

func outputPing(Ping *Ping) {

	if Ping.Success > 0 {
		Ping.TAvg = Ping.TTime / Ping.Success
	}
	Ping.TTime2 /= Ping.Success

	if Ping.Success > 0 {
		Ping.TDev = math.Sqrt(Ping.TTime2 - Ping.TAvg*Ping.TAvg)
	}
	Ping.Percent = (Ping.Failed / Ping.Sent) * 100

	fmt.Println("\n---", Ping.IP, "ping statistics ---")
	fmt.Printf("%v packets transmitted, %v received, %.2f%% packet loss, time %vms\n", Ping.Sent, Ping.Success, Ping.Percent, Ping.TTime)
	fmt.Printf("rtt min/avg/max/mdev = %.3f/%.3f/%.3f/%.3f ms\n", Ping.TMin, Ping.TAvg, Ping.TMax, Ping.TDev)
	fmt.Printf("Target is using cjdns version %v\n", Ping.Version)
}

func main() {
	//Define the flags, and parse them
	//Clearly a hack but it works for now
	//TODO(inhies): Re-implement flag parsing so flags can have multiple meanings based on the base command (ping, route, etc)
	if len(os.Args) <= 1 {
		usage()
		return
	} else if len(os.Args) == 2 {
		if string(os.Args[1]) == "--help" {
			fs.PrintDefaults()
			return
		}
	} else {
		fs.Parse(os.Args[2:])
	}

	command := os.Args[1]

	//read the config
	// TODO: check ./cjdroute.conf /etc/cjdroute.conf ~/cjdroute.conf ~/cjdns/cjdroute.conf maybe ~/cjdns/build/cjdroute.conf
	conf, err := config.LoadMinConfig(File)

	if err != nil || len(conf.Admin.Password) == 0 {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Attempting to connect to cjdns...")
	user, err := admin.Connect(conf.Admin.Bind, conf.Admin.Password) //conf.Admin.Bind
	if err != nil {
		if e, ok := err.(net.Error); ok {
			if e.Timeout() {
				fmt.Println("\nConnection timed out")
			} else if e.Temporary() {
				fmt.Println("\nTemporary error (not sure what that means!)")
			} else {
				fmt.Println("\nUnable to connect to cjdns:", e)
			}
		} else {
			fmt.Println("\nError:", err)
		}
		return
	}

	println("Connected")
	defer user.Conn.Close()
	arguments := fs.Args()
	data := arguments[fs.NFlag()-fs.NFlag():]

	//Setup variables now so that if the program is killed we can still finish what we're doing
	ping := &Ping{}
	var loggingStreamID string

	// capture ctrl+c 
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			fmt.Printf("\n")
			if command == "log" {
				//unsubscribe from logging
				_, err := admin.AdminLog_unsubscribe(user, loggingStreamID)
				if err != nil {
					fmt.Printf("%v\n", err)
					return
				}
			}
			if command == "ping" {
				//stop pinging and print results
				outputPing(ping)
			}
			//close all the channels
			for _, c := range user.Channels {
				close(c)
			}
			user.Conn.Close()
			return
		}
	}()

	switch command {

	case versionsCmd:
		// TODO: ping all nodes in the routing table and get their versions
		// git log -1 --date=iso --pretty=format:"%ad" <hash>
	case killCmd:
		_, err := admin.Core_exit(user)
		if err != nil {
			fmt.Printf("%v\n", err)
			return
		}
		alive := true
		for ; alive; alive, _ = admin.SendPing(user, 1000) {
			runtime.Gosched() //play nice
		}
		println("cjdns is shutting down...")

	case dumpCmd:
		// TODO: add flag to show zero link quality routes, by default hide them
		table := getTable(user)
		k := 1
		for _, v := range table {
			if v.Link >= 1 {
				fmt.Printf("%d IP: %v -- Version: %d -- Path: %s -- Link: %.0f\n", k, v.IP, v.Version, v.RawPath, v.Link)
				k++
			}
		}

	case traceCmd:
		println("Doh! This really needs to get added...")

	case "memory":
		println("Bye bye cjdns! This command causes a crash...")
		response, err := admin.Memory(user)
		if err != nil {
			fmt.Printf("%v\n", err)
			return
		}
		fmt.Println(response)
	case routeCmd:
		var target string
		if len(data) > 0 {
			if strings.Count(data[0], ":") > 1 {
				target = padIPv6(net.ParseIP(data[0]))
				if len(target) != 39 {
					fmt.Println("Invalid IPv6 address")
					return
				}
			} else if strings.Count(data[0], ".") == 3 {
				target = data[0]
				if len(target) != 19 {
					fmt.Println("Invalid cjdns path")
					return
				}
			} else {
				fmt.Println("Invalid IPv6 address or cjdns path")
				return
			}
		} else {
			fmt.Println("You must specify an IPv6 address or cjdns path")
			return
		}
		table := getTable(user)
		for _, v := range table {
			if v.IP == target || v.RawPath == target {
				fmt.Printf("IP: %v -- Version: %d -- Path: %s -- Link: %.0f\n", v.IP, v.Version, v.RawPath, v.Link)
			}
		}
		return
	case pingCmd:
		// TODO: allow input of IP, hex path with and without dots and leading zeros, and binary path
		// TODO: allow pinging of entire routing table
		if len(data) > 0 {
			if strings.Count(data[0], ":") > 1 {
				ping.IP = padIPv6(net.ParseIP(data[0]))
				if len(ping.IP) != 39 {
					fmt.Println("Invalid IPv6 address")
					return
				}
			} else if strings.Count(data[0], ".") == 3 {
				ping.IP = data[0]
				if len(ping.IP) != 19 {
					fmt.Println("Invalid cjdns path")
					return
				}
			} else {
				fmt.Println("Invalid IPv6 address or cjdns path")
				return
			}
		} else {
			fmt.Println("You must specify an IPv6 address or cjdns path")
			return
		}

		if PingCount != defaultPingCount {
			// ping only as much as the user asked for
			for i := 1; i <= PingCount; i++ {
				err := pingNode(user, ping)
				if err != nil {
					fmt.Println(err)
					return
				}
			}
		} else {
			// ping until we're told otherwise
			for {
				err := pingNode(user, ping)
				if err != nil {
					fmt.Println(err)
					return
				}
			}
		}
		outputPing(ping)

	case logCmd:
		var response chan map[string]interface{}
		response, loggingStreamID, err = admin.AdminLog_subscribe(user, LogFile, LogLevel, LogFileLine)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		format := "%d %d %s %s:%d %s\n" // TODO: add user formatted output
		counter := 1
		for {
			input, ok := <-response
			if !ok {
				break
			}
			fmt.Printf(format, counter, input["time"], input["level"], input["file"], input["line"], input["message"])
			counter++
		}
	case "test":

		table := getTable(user)
		//for _, v := range table {
		//	if v.IP == target || v.RawPath == target {
		//		fmt.Printf("IP: %v -- Version: %d -- Path: %s -- Link: %.0f\n", v.IP, v.Version, v.RawPath, v.Link)
		//	}
		//}

		host := data[0] //"fc20:b48d:4ff4:275a:a86f:48d9:2cff:000c"
		for i := range table {

			if table[i].IP != host {
				//If host is supplied, and the
				//IP doesn't match, ignore it.
				//println("nope")
				continue
			}
			if table[i].Link < 1 {
				//fmt.Println(table[i].Link)
				continue
			}
			println("next")
			sPath1 := strings.Replace(table[i].RawPath, ".", "", -1)
			bPath1, _ := hex.DecodeString(sPath1)
			fullPath := binary.BigEndian.Uint64(bPath1)

			//hopCount:
			//recurse:
			for ii := range table {
				if ii == i {
					//Skip the self route
					continue
				}
				//println(table[i].RawPath)

				sPath2 := strings.Replace(table[ii].RawPath, ".", "", -1)
				bPath2, _ := hex.DecodeString(sPath2)
				path2 := binary.BigEndian.Uint64(bPath2)

				candPath := path2

				g := 64 - uint64(math.Log2(float64(candPath)))
				h := uint64(uint64(0xffffffffffffffff) >> g)

				if h&fullPath == h&candPath {

					//hops++
					fmt.Printf(" %b is a sub-path of\n%b\n\n", candPath, fullPath)
					//	path1 = path2
					//break recurse
					//if hops > maxHops {
					//	println("something")
					//	break hopCount
					//}
				} else {
					//fmt.Printf(" %b is not a sub-path of\n%b\n\n", candPath, fullPath)
				}
			}

			//fmt.Println(table[i])
		}
	default:
		fmt.Println("Invalid command", command)
		usage()
		return
	}
}

/*
case "test":
		table := getTable(user)
		host := "fcf1:b5d5:d0b4:c390:9db2:3f5e:d2d2:bff2"

		for _, v := range table {
			if v.IP == host {
				sPath1 := strings.Replace(v.RawPath, ".", "", -1)
				bPath1, _ := hex.DecodeString(sPath1)
				path := binary.BigEndian.Uint64(bPath1)

				result, err := subPath(table, path)
				if err != nil {
					fmt.Println(err)
				}
				if result != 0 {
					println("found  a path")
				}
			}
		}

	default:
		fmt.Println("Invalid command", command)
		usage()
		return
	}
}
*/
func subPath(table []*Route, fullPath uint64) (candPath uint64, err error) {

	//for i := range table {

	//if table[i].IP != host {
	//	//If host is supplied, and the
	//	//IP doesn't match, ignore it.
	//	//println("nope")
	//	continue
	//}
	//if table[i].Link < 1 {
	//	//fmt.Println(table[i].Link)
	//	continue
	//}
	//println("next")

	//hopCount:
	//recurse:
	for ii := range table {

		//println(table[i].RawPath)

		sPath2 := strings.Replace(table[ii].RawPath, ".", "", -1)
		bPath2, _ := hex.DecodeString(sPath2)
		//Read the []byte into a unit64
		path2 := binary.BigEndian.Uint64(bPath2)

		candPath := path2
		if candPath == fullPath {
			//	//Skip the self route
			continue
		}
		g := uint64(64 - math.Min(math.Log2(float64(candPath)), math.Log2(float64(fullPath))))
		h := uint64(uint64(0xffffffffffffffff) >> g)
		if h&fullPath == h&candPath {

			//hops++
			fmt.Printf("%b is a sub-path of\n%b\n\n", candPath, fullPath)

			return candPath, nil
			//	path1 = path2
			//break recurse
			//if hops > maxHops {
			//	println("something")
			//	break hopCount
			//}
		}
	}
	return 0, fmt.Errorf("Not found")
	//fmt.Println(table[i])
	//}
}

// Fills out an IPv6 address to the full 32 bytes
func padIPv6(ip net.IP) string {
	raw := hex.EncodeToString(ip)
	parts := make([]string, len(raw)/4)
	for i := range parts {
		parts[i] = raw[i*4 : (i+1)*4]
	}
	return strings.Join(parts, ":")
}

// Dumps the entire routing table and structures it
func getTable(user *admin.Admin) (table []*Route) {
	page := 0
	var more int64
	table = make([]*Route, 0)
	for more = 1; more != 0; page++ {
		response, err := admin.NodeStore_dumpTable(user, page)
		if err != nil {
			fmt.Printf("%v\n", err)
			return
		}
		// If an error field exists, and we have an error, return it
		if _, ok := response["error"]; ok {
			if response["error"] != "none" {
				err = fmt.Errorf(response["error"].(string))
				fmt.Printf("Error: %v\n", err)
				return
			}
		}
		//Thanks again to SashaCrofter for the table parsing
		rawTable := response["routingTable"].([]interface{})
		for i := range rawTable {
			item := rawTable[i].(map[string]interface{})
			rPath := item["path"].(string)
			sPath := strings.Replace(rPath, ".", "", -1)
			bPath, err := hex.DecodeString(sPath)
			if err != nil || len(bPath) != 8 {
				//If we get an error, or the
				//path is not 64 bits, discard.
				//This should also prevent
				//runtime errors.
				continue
			}
			table = append(table, &Route{
				IP:      item["ip"].(string),
				Path:    string(bPath),
				RawPath: rPath,
				RawLink: item["link"].(int64),
				Link:    float64(item["link"].(int64)) / magicalLinkConstant,
				Version: item["version"].(int64),
			})

		}

		if response["more"] != nil {
			more = response["more"].(int64)
		} else {
			break
		}
	}

	return
}

// Pings a node and generates statistics
func pingNode(user *admin.Admin, ping *Ping) (err error) {
	response, err := admin.RouterModule_pingNode(user, ping.IP, PingTimeout)

	if err != nil {
		return
	}

	ping.Sent++
	if response.Error == "" {
		if response.Result == "timeout" {
			fmt.Printf("Timeout from %v after %vms\n", ping.IP, response.Time)
			ping.Failed++
		} else {
			fmt.Printf("Reply from %v %vms\n", ping.IP, response.Time)
			ping.Success++
			ping.CTime = float64(response.Time)
			ping.TTime += ping.CTime
			ping.TTime2 += ping.CTime * ping.CTime
			if ping.TMin == 0 {
				ping.TMin = ping.CTime
			}
			if ping.CTime > ping.TMax {
				ping.TMax = ping.CTime
			}
			if ping.CTime < ping.TMin {
				ping.TMin = ping.CTime
			}

			if ping.Version == "" {
				ping.Version = response.Version
			}
			if ping.Version != response.Version {
				//not likely we'll see this happen but it doesnt hurt to be prepared
				println("Host is sending back mismatched versions")
			}
		}
	} else {
		ping.Failed++
		return fmt.Errorf(response.Error)
	}
	return
}
