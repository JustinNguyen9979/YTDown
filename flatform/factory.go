// platform/factory.go
package platform

func NewManager() Manager {
	return newOSManager()
}
