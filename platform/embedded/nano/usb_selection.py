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
