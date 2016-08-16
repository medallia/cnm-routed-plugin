# docker-routed-plugin

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

To launch a container using the routed mode, you need to specify it and add a label containing the list of IP addresses you want to assign to a particular container.


```bash
docker run --it --net=routed --label io.docker.network.endpoint.ip4addresses="192.168.13.1,10.112.20.2" ubuntu
```

Also an 'ip-address' option is available to supply the addresses
```bash
docker run --it --net=routed --ip-address="192.168.13.1,10.112.20.2" ubuntu
```

## IP tables integration

Works with the routed driver. Allows to specify what IPs are allowed to connect
to the container.

You specify it via the container label "io.docker.network.endpoint.ingressAllowed".
For example:

```bash
docker run -it --net=routed --ip-address=192.168.13.13 --label io.docker.network.endpoint.ingressAllowed="1.1.1.1/24,2.2.2.2" ubuntu /bin/bash
```

The parameter accepts a comma separated list of values, which can be:

  * Single IP
  * IP Net (CIDR)
  * IP Range (IP-IP)

The host machine is expected to have the following IP Chains for this feature to work.
(DCIB adds them in DCs)

  * CONTAINERS: Where references to container specific chains are added.
  This is supposed to be referenced from the FORWARD chain.
  * CONTAINER-REJECT: What the container specific chain jumps to in case of rejection.

For local development, you can execute these commands:

```bash
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

If the label is not specified, there is no ingress restriction enforced.

### Auto volume mount (NFS/Ceph)

```bash
docker run -v 10.112.12.13//foo:/foo:nfs,rw ubuntu
```

```bash
docker run -v ceph-volume-foo:/foo:ceph,rw ubuntu
```

## Contributing

### Development env installation using Vagrant

1. Create working dir for docker-routed-plugin

```
$ mkdir -p ~/repos/docker-devel/go/src/github.com/medallia
$ cd ~/repos/docker-devel/go/src/github.com/medallia
$ git clone http://github.com/medallia/docker-routed-plugin
$ cp docker-routed-plugin/Vagrantfile ~/repos/docker-devel
```

2. Install vagrant (for OSX)

```
$ brew cask install vagrant
$ vagrant plugin install vagrant-reload
$ vagrant plugin install vagrant-vbguest
```

3. Initialize your vagrant VM

```
$ cd ~/repos/docker-devel
$ vagrant init ubuntu/trusty64
$ vagrant ssh
```

4. Set up you GO environment in the VM (see https://www.digitalocean.com/community/tutorials/how-to-install-go-1-6-on-ubuntu-14-04)

```
$ sudo apt-get update
$ sudo curl -O https://storage.googleapis.com/golang/go1.6.linux-amd64.tar.gz
$ sudo tar -xvf go1.6.linux-amd64.tar.gz
$ sudo mv go /usr/local
$ echo "export GOPATH=/vagrant/go:/vagrant/go/src/github.com/medallia/docker-routed-plugin" >> ~/.profile
$ echo "export PATH=$PATH:/vagrant/go/bin:/usr/local/go/bin" >> ~/.profile
$ source ~/.profile
```

5. Install govendor in the VM

```
$ go get -u github.com/kardianos/govendor
```

6. Install delve (go debugging tool) in the VM

```
$ go get github.com/derekparker/delve/cmd/dlv
```

7. Install docker in the VM (see https://docs.docker.com/engine/installation/linux/ubuntulinux/)

```
$ sudo apt-get install apt-transport-https ca-certificates
$ sudo apt-key adv --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 58118E89F3A912897C070ADBF762215
$ sudo vim /etc/apt/sources.list.d/docker.list # add deb https://apt.dockerproject.org/repo ubuntu-trusty main
$ sudo apt-get update
$ sudo apt-get purge lxc-docker
$ apt-cache policy docker-engine
$ sudo apt-get update
$ sudo apt-get install linux-image-extra-$(uname -r)
$ sudo apt-get install docker-engine
$ sudo service docker start
$ sudo usermod -aG docker $USER
```

8. Finish setting up your development env in the VM

```
$ cd
$ mkdir repos
$ cd repos
$ ln -s /vagrant/go/src/github.com/medallia/docker-routed-plugin docker-plugin
$ curl -fsSLO https://get.docker.com/builds/Linux/x86_64/docker-1.12.0.tgz && tar --strip-components=1 -xvzf docker-1.12.0.tgz docker/docker && mv docker docker.ok && rm docker-1.12.0.tgz
$ git clone http://github.com/docker/docker && cd docker && git checkout 8eab29edd820017901796eb60d4bea28d760f16 && cd -
$ cp docker-plugin/docker-build.sh .
$ sudo bash docker-build.sh
```

### Usage

1. Build plugin docker image and run the driver

```
$ vagrant ssh
$ cd ~/repos/docker-routed-plugin
$ make docker-run
```

2. Test network creation (in another terminal)

```
$ vagrant ssh
$ docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.46.0.0/16  mine
$ docker run -ti --net=mine --ip 10.46.1.7 debian:jessie sh
```

### Debugging with Delve

For info on Delve see https://blog.gopheracademy.com/advent-2015/debugging-with-delve
and https://github.com/derekparker/delve/tree/master/Documentation/cli

1. Run the plugin in a terminal

```
$ vagrant ssh
$ docker run -ti --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins test/routed-plugin -log-level debug
```

2. In another terminal, attach delve to the plugin process

For breakpoint syntax see https://github.com/derekparker/delve/issues/528

```
$ vagrant ssh
$ ps afx | grep docker
$ sudo su
# export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/vagrant/go/bin:/usr/local/go/bin:/vagrant/go/bin:/usr/local/go/bin
# dlv attach <pid>
(dlv) break /routed.*CreateNetwork/
(dlv) break /routed.*DeleteNetwork/
(dlv) break /routed.*NewDriver/
```

3. From yet another terminal, create a routed network in a docker container

```
$ vagrant ssh
$ docker network create --internal --driver=routed --subnet 10.46.0.0/16  mine
```

4. Now you can debug in the delve terminal

```
(dlv) continue
```

### Adding dependencies via govendor

```
$ vagrant ssh
$ cd /vagrant/go/src/github.com/medallia/docker-routed-plugin/
$ govendor fetch github.com/docker/libnetwork
```

### Additional information

To contribute with the development of this plugin it is recommended that you go
through the following documentation:

* https://github.com/docker/libnetwork/blob/master/docs/design.md
* https://github.com/docker/libnetwork/blob/master/docs/remote.md
