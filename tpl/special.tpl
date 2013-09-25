
{{define "net_IP"}}
	{{.Dest}} = net.ParseIP({{.Src}})
{{end}}

{{define "net_IP_ptr"}}
	if {{.Src}}==nil {
	  	{{.Dest}} = nil
	} else {
		{{.Dest}} = &(net.ParseIP(*{{.Src}}))
	}
{{end}}

{{define "net_IPNet"}}
	_, nnet, err := net.ParseCIDR({{.Src}})
	if err!=nil {
		return nil, err
	}
	{{.Dest}} = *nnet
{{end}}

{{define "net_IPNet_ptr"}}
	if {{.Src}}==nil {
	  	{{.Dest}} = nil
	} else {
		_, nnet, err := net.ParseCIDR(*{{.Src}})
		if err!=nil {
			return nil, err
		}
		{{.Dest}} = nnet
	}
{{end}}

{{define "net_HardwareAddr"}}
	hw, err := net.ParseMAC({{.Src}})
	if err!=nil {
		return nil, err
	}
	{{.Dest}} = hw
{{end}}

{{define "net_HardwareAddr_ptr"}}
	if {{.Src}}==nil {
	  	{{.Dest}} = nil
	} else {
		hw, err := net.ParseMAC(*{{.Src}})
		if err!=nil {
			return nil, err
		}
		{{.Dest}} = &hw
	}
{{end}}
