package store

import (
	"fmt"
)

func (p Plan) Price() string {
	if p.Price1m.Valid {
		f, _ := p.Price1m.Float64Value()
		if f.Valid && f.Float64 > 0 {
			return fmt.Sprintf("%.2f", f.Float64)
		}
	}
	if p.MonthlyPrice.Valid {
		f, _ := p.MonthlyPrice.Float64Value()
		if f.Valid && f.Float64 > 0 {
			return fmt.Sprintf("%.2f", f.Float64)
		}
	}
	return "0.00"
}

func (p Plan) Price1mStr() string {
	return p.Price()
}

func (p Plan) Price3mStr() string {
	if p.Price3m.Valid {
		f, _ := p.Price3m.Float64Value()
		if f.Valid && f.Float64 > 0 {
			return fmt.Sprintf("%.2f", f.Float64)
		}
	}
	return ""
}

func (p Plan) Price6mStr() string {
	if p.Price6m.Valid {
		f, _ := p.Price6m.Float64Value()
		if f.Valid && f.Float64 > 0 {
			return fmt.Sprintf("%.2f", f.Float64)
		}
	}
	return ""
}

func (p Plan) Price12mStr() string {
	if p.Price12m.Valid {
		f, _ := p.Price12m.Float64Value()
		if f.Valid && f.Float64 > 0 {
			return fmt.Sprintf("%.2f", f.Float64)
		}
	}
	return ""
}

func (p Plan) TrafficLimitBytes() int64 {
	if p.TrafficLimit.Valid && p.TrafficLimit.Int64 > 0 {
		return p.TrafficLimit.Int64
	}
	if p.TrafficLimitGb.Valid && p.TrafficLimitGb.Int32 > 0 {
		return int64(p.TrafficLimitGb.Int32) * 1024 * 1024 * 1024
	}
	return 0
}

func (p Plan) TrafficGB() int32 {
	if p.TrafficLimitGb.Valid && p.TrafficLimitGb.Int32 > 0 {
		return p.TrafficLimitGb.Int32
	}
	if p.TrafficLimit.Valid && p.TrafficLimit.Int64 > 0 {
		return int32(p.TrafficLimit.Int64 / (1024 * 1024 * 1024))
	}
	return 0
}

func (p Plan) MaxDevicesCount() int32 {
	if p.MaxDevices.Valid && p.MaxDevices.Int32 > 0 {
		return p.MaxDevices.Int32
	}
	if p.DeviceLimit.Valid && p.DeviceLimit.Int32 > 0 {
		return p.DeviceLimit.Int32
	}
	return 3
}

func (p Plan) HasProtocol(proto string) bool {
	for _, pr := range p.Protocols {
		if pr == proto {
			return true
		}
	}
	return false
}
