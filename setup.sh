setupTun() {
    echo "setting up tun0 in 'red'"
    sudo ip netns exec red ip link set lo up
    sudo ip netns exec red ip tuntap add mode tap name tun0
    sudo ip netns exec red ip link set tun0 mtu 65521
    sudo ip netns exec red ip link set tun0 up
    sudo ip netns exec red ip addr add 10.0.2.100/24 dev tun0
    sudo ip netns exec red ip route add 0.0.0.0/0 via 10.0.2.15 dev tun0
}

if [ ! -f /var/run/netns/red ]
then
    echo "setting up 'red' namespace"
    sudo ip netns add red
    setupTun
else 
    tun0Matches=$(sudo ip netns exec red ip addr | grep tun0)
    if [ -z "${tun0Matches// }" ] 
    then
        setupTun
    fi
fi

go build .
sudo ./tun red