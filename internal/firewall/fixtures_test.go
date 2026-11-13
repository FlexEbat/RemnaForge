package firewall

// Recorded outputs of the tools on real servers (addresses changed). The
// tests parse these instead of trusting a hand-made imitation.

const ufwActive = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
80/tcp                     ALLOW IN    Anywhere
443/tcp                    ALLOW IN    Anywhere
8443/tcp                   DENY IN     Anywhere
2222                       ALLOW IN    198.51.100.7
6000:6003/tcp              ALLOW IN    Anywhere
53                         ALLOW IN    Anywhere
22/tcp (v6)                ALLOW IN    Anywhere (v6)
80/tcp (v6)                ALLOW IN    Anywhere (v6)
`

const ufwInactive = "Status: inactive\n"

const ufwOpenAll = `Status: active
Logging: on (low)
Default: allow (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
25/tcp                     DENY IN     Anywhere
`

const firewalldList = `public (active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources: 
  services: dhcpv6-client ssh http
  ports: 443/tcp 9000/udp 6000-6001/tcp
  protocols: 
  forward: yes
  masquerade: no
  forward-ports: 
  source-ports: 
  icmp-blocks: 
  rich rules: 
`

const firewalldRich = `public (active)
  target: default
  services: ssh
  ports: 
  rich rules: 
	rule family="ipv4" source address="192.0.2.0/24" port port="443" protocol="tcp" accept
`

const iptablesInputDrop = `-P INPUT DROP
-A INPUT -i lo -j ACCEPT
-A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT
-A INPUT -p tcp -m multiport --dports 80,443 -j ACCEPT
-A INPUT -p tcp -m tcp --dport 8443 -j DROP
`

const iptablesInputUFW = `-P INPUT DROP
-A INPUT -j ufw-before-logging-input
-A INPUT -j ufw-before-input
-A INPUT -j ufw-after-input
`

const iptablesInputOpen = "-P INPUT ACCEPT\n"

const nftRuleset = `table inet filter {
	chain input {
		type filter hook input priority filter; policy drop;
		ct state established,related accept
		iif "lo" accept
		tcp dport { 22, 80, 443 } accept
		tcp dport 8443 drop
		tcp dport 5000-5002 accept
	}
	chain forward {
		type filter hook forward priority filter; policy accept;
		tcp dport 9999 drop
	}
}
table ip nat {
	chain DOCKER {
		iifname "docker0" counter return
	}
	chain POSTROUTING {
		type nat hook postrouting priority srcnat; policy accept;
		oifname != "docker0" ip saddr 172.17.0.0/16 counter masquerade
	}
}
`

const nftDockerOnly = `table ip nat {
	chain POSTROUTING {
		type nat hook postrouting priority srcnat; policy accept;
		oifname != "docker0" ip saddr 172.17.0.0/16 counter masquerade
	}
}
table ip filter {
	chain FORWARD {
		type filter hook forward priority filter; policy drop;
		counter jump DOCKER-USER
	}
}
`

const nftJump = `table inet filter {
	chain input {
		type filter hook input priority filter; policy drop;
		tcp dport 22 accept
		jump custom
	}
}
`

const iptablesVersionNft = "iptables v1.8.9 (nf_tables)\n"
const iptablesVersionLegacy = "iptables v1.8.7 (legacy)\n"

const natDocker = `-P PREROUTING ACCEPT
-P INPUT ACCEPT
-P OUTPUT ACCEPT
-P POSTROUTING ACCEPT
-N DOCKER
-A PREROUTING -m addrtype --dst-type LOCAL -j DOCKER
-A OUTPUT ! -d 127.0.0.0/8 -m addrtype --dst-type LOCAL -j DOCKER
-A POSTROUTING -s 172.17.0.0/16 ! -o docker0 -j MASQUERADE
-A DOCKER -i docker0 -j RETURN
`

const natNone = `-P PREROUTING ACCEPT
-P INPUT ACCEPT
-P OUTPUT ACCEPT
-P POSTROUTING ACCEPT
`

const ssOutput = `LISTEN 0      4096         0.0.0.0:80         0.0.0.0:*    users:(("docker-proxy",pid=1234,fd=4))
LISTEN 0      4096         0.0.0.0:443        0.0.0.0:*    users:(("docker-proxy",pid=1250,fd=4))
LISTEN 0      128          0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=701,fd=3))
LISTEN 0      4096   127.0.0.53%lo:53         0.0.0.0:*    users:(("systemd-resolve",pid=402,fd=14))
LISTEN 0      4096       127.0.0.1:2222       0.0.0.0:*    users:(("xray",pid=2301,fd=7))
LISTEN 0      128             [::]:22            [::]:*    users:(("sshd",pid=701,fd=4))
LISTEN 0      4096            [::]:80            [::]:*    users:(("docker-proxy",pid=1241,fd=4))
`
