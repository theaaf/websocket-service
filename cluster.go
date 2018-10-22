package websocketservice

type Cluster interface {
	// Returns the address of the local node. Other nodes in the cluster should be able to send
	// messages to this node using this address.
	Address() Address

	SendServiceRequest(address Address, r *ServiceRequest) error
}
