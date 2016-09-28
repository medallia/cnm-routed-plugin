#!/usr/bin/env bats

function setup {

  # run plugin
  run docker rm routed-test
  docker run -d --name routed-test --privileged --net=host -v /run/docker/plugins:/run/docker/plugins routed-plugin --gateway 10.100.0.1 --mtu 9000

  # create network
  run docker network rm test
  docker network create --internal --driver=net-routed --ipam-driver=ipam-routed --subnet 10.1.0.0/16 test
}

function teardown {

  # remove network test
  run docker network rm test

  # kill plugin process
  docker stop routed-test
  docker rm routed-test
}

@test "Check network setup" {

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine ip addr
  [[ "$status" -eq 0 ]]
  [[ "{$output}" == *"10.1.2.3"* ]]

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine ip route
  [[ "$status" -eq 0 ]]
  [[ "${output}" == *"default via 10.100.0.1 dev eth0"* ]]
}

@test "Check container communication" {

  run docker rm routed-nc-test

  docker run -d --name routed-nc-test --net=test --ip=10.1.1.1 alpine nc -l -p 9999 -s 10.1.1.1
  run docker run --rm --net=test --ip=10.1.1.2 alpine sh -c 'echo foo|nc 10.1.1.1 9999'
  [[ "$status" -eq 0 ]]

  run docker logs $(docker ps -qa --filter name=routed-nc-test)
  [[ "${output}" == *"foo"* ]]

  docker stop routed-nc-test
  docker rm routed-nc-test
}

# ARP table should only contain entries for the default gw mac address
@test "Check arp table" {

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine sh -c "ping -c 3 8.8.8.8 > /dev/null; ping -c 3 8.8.4.4 > /dev/null; sleep 2; arp -a"
  [[ "$status" -eq 0 ]]
  [[ "${output}" == *"10.100.0.1"* ]]
  [[ "${output}" != *"8.8.8.8"* ]]
  [[ "${output}" != *"8.8.4.4"* ]]
}

@test "Check mtu" {

  run docker run --rm -ti --net=test --ip=10.1.2.3 alpine sh -c "ip a"
  [[ "$status" -eq 0 ]]
  # default mtu
  [[ "${output}" == *"mtu 9000"* ]]

  run docker run --rm -ti --net=test --ip=10.1.2.3 --net-opt com.medallia.routed.network.mtu=1500 alpine sh -c "ip a"
  [[ "$status" -eq 0 ]]
  # custom mtu
  [[ "${output}" == *"mtu 1500"* ]]
}

@test "Check IP aliases" {

  run docker rm routed-aliases-test

  docker run -d --name routed-aliases-test -ti --net=test --ip=10.1.2.3 --net-opt com.medallia.routed.network.ipAliases="192.168.255.254/32,192.168.255.255/32" alpine sh -c "ip a; sleep 10"

  run ip r
  [[ "$status" -eq 0 ]]
  [[ "${output}" == *"10.1.2.3"* ]]
  [[ "${output}" == *"192.168.255.254"* ]]
  [[ "${output}" == *"192.168.255.255"* ]]

  run docker logs $(docker ps -qa --filter name=routed-aliases-test)
  [[ "$status" -eq 0 ]]
  [[ "${output}" == *"10.1.2.3"* ]]
  [[ "${output}" == *"192.168.255.254/32"* ]]
  [[ "${output}" == *"192.168.255.255/32"* ]]

  docker stop routed-aliases-test
  docker rm routed-aliases-test
}

@test "Check ingress rules" {

  run docker rm routed-iptables-test

  run sh -c "sudo iptables -L | grep CONTAINERS"
  if [ "$status" -ne 0 ]; then
    skip "Skipping... chains are not configured"
  fi

  docker run -d --name routed-iptables-test -ti --net=test --ip=10.1.2.3 --net-opt com.medallia.routed.network.ingressAllowed="192.168.1.0/24,2.2.2.2" alpine sh -c "sleep 10"

  run sudo iptables -L
  [[ "$status" -eq 0 ]]
  [[ "${output}" == *"192.168.1.0/24"* ]]
  [[ "${output}" == *"2.2.2.2"* ]]

  docker stop routed-iptables-test
  docker rm routed-iptables-test
}
