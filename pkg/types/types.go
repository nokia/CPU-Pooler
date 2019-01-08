package types

type PoolConfig struct {
  DeviceBaseName string
  Default []int
  Shared []int
  Exclusive []int
}