"""Shared firmware route/closure markers for native MENU and paired traces."""
import re
import importlib.util
from pathlib import Path
_route_spec=importlib.util.spec_from_file_location('fes_observed_sdm_reader',Path(__file__).resolve().parent.parent/'firmware/nano_sdm_reader_profile.py')
reader_route=importlib.util.module_from_spec(_route_spec);_route_spec.loader.exec_module(reader_route)

H=r'[0-9a-f]{1,16}'
D=r'[0-9]{1,20}'
P=r'(?:NOTICE:[ \t]+)?'
PATTERNS={
    'reader_route':re.compile(P+rf'FES_NANO_READER_ROUTE status=(?P<status>-?{D}) owner=(?P<owner>{H}) stream=(?P<stream>{D}) secure=(?P<secure>[01]) aruser=(?P<aruser>{H}) arprot=(?P<arprot>{H}) bank=(?P<bank>secure) ns_abort=(?P<ns_abort>[01]) source_retained=(?P<source_retained>[01]) source_generation=(?P<source_generation>{D}) generation=(?P<generation>{D}) binding=(?P<binding>[0-9a-f]{{64}})'),
    'scope':re.compile(P+rf'FES_NANO_WINDOW_SCOPE phase=(?P<phase>control|data|close) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H})'),
    'gate':re.compile(P+rf'FES_NANO_WINDOW_GATE status=(?P<status>-?{D}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) bytes=(?P<bytes>{D}) permit=(?P<permit>owned-read|denied)'),
    'master':re.compile(P+rf'FES_NANO_MASTER_STOP status=(?P<status>-?{D}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) reader_reads=(?P<reader_reads>{D}) reader_polls=(?P<reader_polls>{D}) disable=(?P<disable>[01]) probe_polls=(?P<probe_polls>{D}) cancel=(?P<cancel>[01]) complete=(?P<complete>[01]) data=active'),
    'closure':re.compile(P+rf'FES_NANO_WINDOW_CLOSURE owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) hardware_drained=(?P<hardware_drained>{H}) data_remained_contained=(?P<data_remained_contained>[01]) producers_quiescent=(?P<producers_quiescent>[01]) fabric_acknowledged=(?P<fabric_acknowledged>[01])'),
    'reader_drain':re.compile(P+rf'FES_NANO_WINDOW_READER_DRAIN owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) reader_generation=(?P<reader_generation>{D}) request_generation=(?P<request_generation>{D}) cookie=(?P<cookie>{D}) ack_generation=(?P<ack_generation>{D}) ack_cookie=(?P<ack_cookie>{D}) status=(?P<status>{H}) pending=(?P<pending>{H})'),
}
HEX={'owner','reset','crc32','hardware_drained','idr0','idr1','aidr','s_idr1','cr0','ack','gbpa','agbpa',
     'aruser','arprot','s_cr0','s_ack','s_init','s_gbpa','s_agbpa','enable','base','baseext','limit','limitext','published_crc32','read_crc32','pending','state','version','pins','soft','error_location','error_details'}
TEXT={'binding','bank','permit','phase','package_id','build_id','frame_sha256','access_map_sha256'}


def check_route(take,require,*,owner,generation,reset,memory,with_gate,retained_reader=None,retained_source_generation=None):
    """Check the same sealed route for bounded probes and request services."""
    def owned(value,generation,reset):
        require(value['owner']==owner and value['generation']==generation and value['reset']==reset,
                'window route changed owner, generation or reset')
    if retained_reader is not None:
        reader_route.checked_binding(retained_reader)
        source=4 if retained_source_generation is None else retained_source_generation
        require(type(source) is int and 4<=source<(1<<64)-3, 'bounded retained source generation required')
        require(memory['bytes']==0x800000 and memory['bus_base']==0x95000000 and
                (generation,reset)==((source+1,13) if with_gate else (source,15)), 'retained reader is outside its sealed MENU phase')
    else:
        require(retained_source_generation is None,'source generation requires the sealed retained reader')
    translation=take('smmu')
    require(translation==dict(status=0,reads=26,idr0=0x080f7e3f,idr1=0x0e739d18,aidr=1,s_idr1=0x80000018,
        cr0=0,ack=0,gbpa=0x100000 if retained_reader is not None else 0,agbpa=0,s_cr0=0,s_ack=0,s_init=0,s_gbpa=0,s_agbpa=0,
        identified=1,settled=1,bypass_controls_clear=0 if retained_reader is not None else 1),'window use-point translation is not the selected sealed route')
    firewall=take('ddr_firewall');owned(firewall,generation,reset)
    require(firewall['status']==0 and firewall['reads']==66 and firewall['mpfe_reset']==0 and
            firewall['base']==memory['bus_base'] and firewall['bytes']==memory['bytes'] and not firewall['enable']&~255,
            'window use-point firewall observation differs')
    covered=False
    for i in range(8):
        region=take('ddr_region')
        require(region['index']==i and region['base']&65535==0 and region['limit']&65535==65535 and
                max(region['base'],region['limit'])<=0xffffffff and
                max(region['baseext'],region['limitext'])<=255,'window firewall region layout is unknown')
        low=region['baseext']<<32|region['base'];high=region['limitext']<<32|region['limit']
        require(not firewall['enable']>>i&1 or low<=high,'enabled window firewall region is inverted')
        covered|=bool(firewall['enable']>>i&1 and low<=memory['bus_base'] and high>=memory['bus_base']+memory['bytes']-1)
    require(take('ddr_firewall_end')==dict(layout_known=1,window_covered=1) and covered,
            'firewall does not cover the complete window')
    if with_gate:
        if retained_reader is not None:
            route=take('reader_route')
            require(route==dict(status=0,owner=owner,stream=1,secure=1,aruser=0x3e0,arprot=3,bank='secure',
                    ns_abort=1,source_retained=1,source_generation=source,generation=generation,
                    binding=reader_route.digest(retained_reader)), 'sealed retained reader route proof differs')
        gate=take('gate');owned(gate,generation,reset)
        require(gate['status']==0 and gate['bytes']==memory['bytes'] and gate['permit']=='owned-read','window gate denied access')
