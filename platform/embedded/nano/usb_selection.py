"""Explicit native USB scope carried by source-bound build receipts."""

def mode(record, program_key='program'):
    shared=record.get('usb_shared',False)
    control=record.get('usb_control',False)
    if type(shared) is not bool or type(control) is not bool:
        raise ValueError('USB selection must use boolean control/shared fields')
    program=record.get(program_key)
    if shared:
        if program!='fpgaload' or any(record.get(key) is not True for key in
                ('tenfoot_serial','selected_programs','computer_reload','computer_storage_return')):
            raise ValueError('shared USB requires selected Tenfoot computer loads and SD return')
        if any(record.get(key,False) is not False for key in
                ('window_memory','window_read','window_present','window_storage_return','window_reload','window_service','smmu_observe')) or record.get('hps_ddr_read') is not None:
            raise ValueError('shared USB cannot inherit another native memory/observation profile')
    selected=program=='usbcheck' or shared
    if control!=selected:
        raise ValueError('USB lease selection differs from native program')
    protocol=record.get('usb_input')
    if selected and protocol not in ('boot-keyboard','gamepad') or not selected and protocol is not None:
        raise ValueError('USB input protocol differs from explicit selection')
    return selected,shared


def contained_probe(record, program_key='program'):
    """USB inspection may observe a held-ROM shell without selecting its controls."""
    if record.get(program_key)!='usbcheck':
        return False
    selected,shared=mode(record,program_key)
    if not selected or shared or any(record.get(key,False) is not False for key in
            ('sd_pio','selected_programs','computer_reload','computer_storage_return',
             'tenfoot_serial','initial_tape','window_memory','window_read','window_present',
             'window_storage_return','window_reload','window_service','menu_present','smmu_observe')):
        raise ValueError('USB probe requires a separate contained selection')
    resources=('hps_control','hps_configuration','hps_reader_memory','hps_ddr_read',
               'hps_window_read','hps_menu_read','hps_computer','hps_rom',
               'hps_program_platform','hps_menu_program','cpu_mapping')
    if any(record.get(key) is not None for name in resources for key in (name,name+'_sha256')) or record.get('rom_selection') is not None:
        raise ValueError('USB probe cannot select FPGA, ROM or program resources')
    return True
