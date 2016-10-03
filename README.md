# cnm-routed-plugin

## Features

Provides a transparent way to assign multiple IP addresses to a docker container. It's based on using standard routing
protocols to share the information of where each container is running across a cluster. Thus not needing to have
a distributed storage and separate processes as the source of truth. Currently using the [Quagga](http://www.nongnu.org/quagga) OSPF implementation.

The regular veth pair creation is then replaced for the following sequence of events.

- Creates a pair of veth.
- Moves one to the container namespace.
- Renames the container veth to eth0.
- Adds route to 0.0.0.0/0 via eth0 in container.
- Sets the requested IP addresses to the container eth0.
- Adds route to container IP via veth0 in the host.

Then the route to reach the container addresses is automatically propagated by the enabled routing protocol. In essence, each host in the cluster acts as a router.

The configuration is quite simple, for example the following ospfd.conf file of Quagga allows to route the containers in the networks 10.112.0.0, 192.168.0.0 and 10.255.255.0 using the host eth1 interface. Any container with IP addresses in those networks, regardless the host where they are running, will be able to talk to each other.

```
! Bootstrap Config
router ospf
 ospf router-id 10.112.11.6
 redistribute kernel
 passive-interface default
 no passive-interface eth1
 network 10.112.0.0/12 area 0.0.0.0
 network 192.168.0.0/16 area 0.0.0.0
 network 10.255.255.0/24 area 0.0.0.0
!
log syslog
!
interface eth1
!ip ospf network point-to-point
!
```

To launch a container using the routed mode, you first need to have the routed
driver running in the host. The <gw-ip> argument is the IP address that will
be configured as next hop for the default route inside the container. This IP
can be a virtual (non-assigned) IP address, if the host does ARP proxying, or
correspond to an actual interface in the host.  

```
docker run -ti --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins routed-plugin --gateway <gw-ip> --mtu 9000 --debug
```

Then you will need to register a routed network. Note that it also uses the Ipam routed driver.

```
docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.1.0.0/16 mine
```

Finally, you can run a container attached to the routed network you created previously.
You will need to specify the ip address to assign to the container endpoint using the
--ip label.  

```
docker run -ti --net=mine --ip 10.1.0.2 alpine sh
```

Configuration of IP aliases, iptables ingress rules and NIC MTU is possible using --net-opt options.

```
docker run -ti --net=mynet --ip 10.1.0.2 --net-opt com.medallia.routed.network.ingressAllowed="192.168.1.0/24,2.2.2.2" --net-opt com.medallia.routed.network.mtu=1500 --net-opt com.medallia.routed.network.ipAliases="192.168.255.254/32,192.168.255.255/32" --rm alpine sh
```

Manual configuration of the following iptables rules in the host is necessary for the com.medallia.routed.network.ingressAllowed option to work correctly.

```
sudo iptables -N CONTAINERS
sudo iptables -A CONTAINERS -j RETURN

sudo iptables -N CONTAINER-REJECT
sudo iptables -A CONTAINER-REJECT -p tcp -j REJECT --reject-with tcp-reset
sudo iptables -A CONTAINER-REJECT -j REJECT

sudo iptables -I FORWARD 1 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
sudo iptables -I FORWARD 2 -p icmp -j ACCEPT
sudo iptables -I FORWARD 3 -m state --state INVALID -j DROP
sudo iptables -I FORWARD 4 -j CONTAINERS
```

## Contributing

### Development env installation using Vagrant

1. Create working directory for cnm-routed-plugin

  ```
  mkdir -p ~/repos/docker-devel/go/src/github.com/medallia
  cd ~/repos/docker-devel/go/src/github.com/medallia
  git clone http://github.com/medallia/cnm-routed-plugin
  ```

2. Install vagrant (for OSX)

  ```
  brew cask install vagrant
  vagrant plugin install vagrant-reload
  vagrant plugin install vagrant-vbguest
  ```

3. Initialize your vagrant VM. (Note that the Vagrantfile includes instructions
to configure ARP proxy, ip4 forwarding and iptables chains on VM provision.
If Vagrantfile is modified, then the VM will need to be
re-provisioned using ```vagrant up --provision```)

  ```
  cp cnm-routed-plugin/Vagrantfile ~/repos/docker-devel
  cd ~/repos/docker-devel
  vagrant up
  vagrant ssh
  ```

4. Set up you GO environment in the VM (see https://www.digitalocean.com/community/tutorials/how-to-install-go-1-6-on-ubuntu-14-04)

  ```
  sudo apt-get update
  sudo curl -O https://storage.googleapis.com/golang/go1.6.linux-amd64.tar.gz
  sudo tar -xvf go1.6.linux-amd64.tar.gz
  sudo mv go /usr/local
  echo "export GOPATH=/vagrant/go" >> ~/.profile
  echo "export PATH=$PATH:/vagrant/go/bin:/usr/local/go/bin" >> ~/.profile
  source ~/.profile
  ```

5. Install govendor in the VM

  ```
  sudo apt-get install git
  go get -u github.com/kardianos/govendor
  cd /vagrant/go/src/github.com/medallia/cnm-routed-plugin
  govendor sync
  ```

6. Install docker in the VM (see https://docs.docker.com/engine/installation/linux/ubuntulinux/)

  ```
  sudo apt-get install apt-transport-https ca-certificates
  sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF76221572C52609D  
  sudo sh -c "echo 'deb https://apt.dockerproject.org/repo ubuntu-trusty main' >> /etc/apt/sources.list.d/docker.list"
  sudo apt-get update
  sudo apt-get purge lxc-docker
  apt-cache policy docker-engine
  sudo apt-get update
  sudo apt-get install linux-image-extra-$(uname -r)
  sudo apt-get install docker-engine
  sudo service docker start
  sudo usermod -aG docker $USER
  exit
  ```

7. Update docker to version 1.12.1 inside the VM

  ```
  vagrant ssh
  mkdir -p /vagrant/go/src/github.com/docker
  cd /vagrant/go/src/github.com/docker
  git clone http://github.com/docker/docker
  cd docker
  git checkout tags/v1.12.1
  make binary
  sudo service docker stop
  sudo cp bundles/latest/binary-*/docker* /usr/bin/
  sudo service docker start
  ```

### Usage

1. Build the docker image for the plugin

  ```
  vagrant ssh
  make docker-build
  ```

2. Then launch the routed driver as a docker container

  ```
  make docker-run
  ```

3. In another terminal create a routed network

  ```
  vagrant ssh
  docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.1.0.0/16  mine
  docker run -ti --net=mine --ip 10.1.0.3 alpine sh
  ```

### Testing

1. Install bats

  ```
  vagrant ssh
  git clone https://github.com/sstephenson/bats.git
  cd bats
  sudo ./install.sh /usr/local
  cd /vagrant/go/src/github.com/medallia/cnm-routed-plugin
  ```

2. Run integration tests

  ```
  make integration
  ```

3. Run unit tests with coverage

  ```
  make coverage
  ```

### Debugging with Delve

For info on Delve see https://blog.gopheracademy.com/advent-2015/debugging-with-delve
and https://github.com/derekparker/delve/tree/master/Documentation/cli

1. Install delve (go debugging tool) in the VM

  ```
  go get github.com/derekparker/delve/cmd/dlv
  ```

2. Run the plugin in a terminal

  ```
  vagrant ssh
  docker run --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins routed-plugin --gateway 10.100.0.1 --mtu 9000 --debug
  ```

2. In another terminal, attach delve to the driver process. For breakpoint syntax see https://github.com/derekparker/delve/issues/528

  ```
  vagrant ssh
  sudo su
  export PATH=$PATH:/vagrant/bin:/vagrant/go/bin:/usr/local/go/bin
  export GOPATH=/vagrant/go
  dlv attach $(ps -A -o pid,cmd|grep " ./routed-plugin --debug" | grep -v grep |head -n 1 | awk '{print $1}')
  (dlv) break /routed.*CreateNetwork/
  (dlv) break /routed.*DeleteNetwork/
  (dlv) break /routed.*NewNetDriver/
  ```

3. From yet another terminal, create a routed network and run your container

  ```
  vagrant ssh
  docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.1.0.0/16  mine
  docker run -ti --net=mine --ip 10.1.0.3 alpine sh
  ```

4. Now you can debug in the delve terminal

  ```
  (dlv) continue
  ```

### Adding dependencies via govendor

```
vagrant ssh
cd /vagrant/go/src/github.com/medallia/cnm-routed-plugin/
govendor fetch github.com/docker/libnetwork
```

### Additional information

To contribute with the development of this plugin it is recommended that you go
through the following documentation:

* https://github.com/docker/libnetwork/blob/master/docs/design.md
* https://github.com/docker/libnetwork/blob/master/docs/remote.md
