// Copyright 2012 Google, Inc. All rights reserved.
// Copyright 2009-2011 Andreas Krennmair. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE file in the root of the source
// tree.

package layers

import (
	"code.google.com/p/gopacket"
	"encoding/binary"
)

// ARP is a ARP packet header.
type ARP struct {
	BaseLayer
	AddrType          LinkType
	Protocol          EthernetType
	HwAddressSize     uint8
	ProtAddressSize   uint8
	Operation         uint16
	SourceHwAddress   []byte
	SourceProtAddress []byte
	DstHwAddress      []byte
	DstProtAddress    []byte
}

// LayerType returns LayerTypeARP
func (arp *ARP) LayerType() gopacket.LayerType { return LayerTypeARP }

// DecodeFromBytes decodes the given bytes into this layer.
func (arp *ARP) DecodeFromBytes(data []byte, df gopacket.DecodeFeedback) error {
	arp.AddrType = LinkType(binary.BigEndian.Uint16(data[0:2]))
	arp.Protocol = EthernetType(binary.BigEndian.Uint16(data[2:4]))
	arp.HwAddressSize = data[4]
	arp.ProtAddressSize = data[5]
	arp.Operation = binary.BigEndian.Uint16(data[6:8])
	arp.SourceHwAddress = data[8 : 8+arp.HwAddressSize]
	arp.SourceProtAddress = data[8+arp.HwAddressSize : 8+arp.HwAddressSize+arp.ProtAddressSize]
	arp.DstHwAddress = data[8+arp.HwAddressSize+arp.ProtAddressSize : 8+2*arp.HwAddressSize+arp.ProtAddressSize]
	arp.DstProtAddress = data[8+2*arp.HwAddressSize+arp.ProtAddressSize : 8+2*arp.HwAddressSize+2*arp.ProtAddressSize]

	arpLength := 8 + 2*arp.HwAddressSize + 2*arp.ProtAddressSize
	arp.Contents = data[:arpLength]
	arp.Payload = data[arpLength:]
	return nil
}

// CanDecode returns the set of layer types that this DecodingLayer can decode.
func (arp *ARP) CanDecode() gopacket.LayerClass {
	return LayerTypeARP
}

// NextLayerType returns the layer type contained by this DecodingLayer.
func (arp *ARP) NextLayerType() gopacket.LayerType {
	return gopacket.LayerTypePayload
}

func decodeARP(data []byte, p gopacket.PacketBuilder) error {

	arp := &ARP{}
	return decodingLayerDecoder(arp, data, p)
}