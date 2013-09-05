/*
 * You may redistribute this program and/or modify it under the terms of
 * the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */
package main

import (
	"fmt"
	"github.com/inhies/go-cjdns/cjdns"
)

// Log base 2 of a uint64
func log2x64(number uint64) uint {
    var out uint = 0
    for number != 0 {
        number = number >> 1
        out++
    }
    return out
}

// return true if packets destine for destination go through midPath.
func isBehind(destination uint64, midPath uint64) bool {
    if midPath > destination {
        return false
    }
    mask := ^uint64(0) >> (64 - log2x64(midPath))
    return (destination & mask) == (midPath & mask)
}

// Return true if destination is 1 hop away from midPath
// WARNING: this depends on implementation quirks of the router and will be broken in the future.
// NOTE: This may have false positives which isBehind() will remove.
func isOneHop(destination uint64, midPath uint64) bool {
    if !isBehind(destination, midPath) { return false; }

    // The "why" is here:
    // http://gitboria.com/cjd/cjdns/tree/master/switch/NumberCompress.h#L143
    c := destination >> log2x64(midPath)
    if c&1 != 0 {
        return log2x64(c) == 4
    }
    if c&3 != 0 {
        return log2x64(c) == 7
    }
    return log2x64(c) == 10
}

/**
 * Print the peers of a node.
 * @param user the admin connection
 * @param target the node to get peers for, if it is the switch label 0000.0000.0000.0001
 *               then this node's peers will be gotten.
 */
func doPeers(user *admin.Admin, target Target) {
	table := getTable(user)
	usingPath := false
	var tText string
	if validIP(target.Supplied) {
		hostname, _ := resolveIP(target.Target)
		if hostname != "" {
			tText = target.Supplied + " (" + hostname + ")"
		} else {
			tText = target.Supplied
		}
		// If we were given a path, resolve the IP
	} else if validPath(target.Supplied) {
		usingPath = true
		tText = target.Supplied
		//table := getTable(globalData.User)
		for _, v := range table {
			if v.Path == target.Supplied {
				// We have the IP now
				tText = target.Supplied + " (" + v.IP + ")"

				// Try to get the hostname
				hostname, _ := resolveIP(v.IP)
				if hostname != "" {
					tText = target.Supplied + " (" + v.IP + " (" + hostname + "))"
				}
			}
		}
		// We were given a hostname, everything is already done for us!
	} else if validHost(target.Supplied) {
		tText = target.Supplied + " (" + target.Target + ")"
	}

	fmt.Println("Finding all direct peers of", tText)

  var output []*Route
	for _,node := range table {
    if usingPath && node.Path != target.Supplied {
      continue
    } else if !usingPath && node.IP != target.Target {
      continue
    }
    for _,nodeB := range table {
      if isOneHop(node.RawPath, nodeB.RawPath) || isOneHop(nodeB.RawPath, node.RawPath) {
          for i,existing := range output {
              if existing.IP == nodeB.IP {
                  if existing.RawPath > nodeB.RawPath { table[i] = nodeB; }
                  goto alreadyExists;
              }
          }
          output = append(output, nodeB);
          alreadyExists:
      }
    }
  }

  for _,node := range output {
		hostname, _ := resolveIP(node.IP)
    tText := node.IP
    if hostname != "" {
        tText += " (" + hostname + ")"
    } else {
        tText += "\t";
    }
    tText += "\t\t\t\t";
    for i:=40; i < len(tText) && i < 80; i+=16 {
        tText = tText[0:len(tText)-2];
    }
    fmt.Printf("IP: %v -- Path: %s -- Link: %.0f\n", tText, node.Path, node.Link)
  }
}
