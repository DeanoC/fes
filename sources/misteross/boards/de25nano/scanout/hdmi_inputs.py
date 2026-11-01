"""Derive synchronized scanout I2C inputs and a complete-start handshake."""
import hashlib
import re


def _once(source, pattern, replacement):
    result, count = re.subn(pattern, replacement, source)
    if count != 1:
        raise ValueError('vendor HDMI input derivation changed: ' + pattern)
    return result


def controller_inputs(controller):
    controller = controller.decode()
    controller = _once(controller, r'\bmodule\s+I2C_Controller\s*\(',
                       'module fes_de25_scanout_i2c_controller (I2C_SDAT_IN,')
    for name in ('ACK1', 'ACK2', 'ACK3'):
        controller = _once(controller, r'\b' + name + r'\s*=\s*I2C_SDAT\s*;',
                           name + '=I2C_SDAT_IN;')
    # The remaining raw SDA references are its port, declaration and the
    # unchanged open-drain output assignment. Extra reads must be reviewed.
    if len(re.findall(r'\bI2C_SDAT\b', controller)) != 3:
        raise ValueError('unreviewed raw SDA reference in vendor controller')
    controller = _once(controller, r'(\binout\s+I2C_SDAT\s*;)',
                       r'\1\ninput wire I2C_SDAT_IN;')
    return controller


def derive(config, controller):
    original = {'I2C_HDMI_Config.v': config, 'I2C_Controller.v': controller}
    config = config.decode()
    controller = controller_inputs(controller)
    config = _once(config, r'\bmodule\s+I2C_HDMI_Config\s*\(',
                   'module fes_de25_scanout_hdmi_config (I2C_SDAT_IN,')
    config = _once(config, r'(\binout\s+I2C_SDAT\s*;)',
                   r'\1\ninput wire I2C_SDAT_IN;')
    config = _once(config, r'\bI2C_Controller\s+u0\s*\(',
        'fes_de25_scanout_i2c_controller u0 (.I2C_SDAT_IN(I2C_SDAT_IN),')
    # END starts high. Wait for the controller to acknowledge GO by becoming
    # busy before accepting its next completion; otherwise the first LUT row
    # can be skipped using the completion level from reset/the previous write.
    config = _once(config, r'(\bmI2C_GO\s*<=\s*1\s*;\s*mSetup_ST\s*<=\s*)1\s*;', r'\g<1>3;')
    config = _once(config, r'\bcase\s*\(\s*mSetup_ST\s*\)',
        'case(mSetup_ST)\n3: if (!mI2C_END) mSetup_ST <= 1;')
    # Separate clocked processes must sample the old register values together.
    # Blocking writes in the vendor source race in event-driven simulation.
    # Its synthesized state equations are compared separately with reset
    # modeling; this normalization does not change the divider or LUT.
    controller, sequential = re.subn(r'\b(SDO|SCLK|END|SD|SD_COUNTER|ACK1|ACK2|ACK3)\s*=(?!=)',
                                     r'\1 <=', controller)
    if sequential < 3:
        raise ValueError('vendor HDMI sequential assignment normalization changed')
    files = {'fes_de25_scanout_hdmi_config.v': config.encode(),
             'fes_de25_scanout_i2c_controller.v': controller.encode()}
    sha = lambda data: hashlib.sha256(data).hexdigest()
    return files, {'vendor_source_sha256': {n: sha(v) for n, v in original.items()},
        'derived_source_sha256': {n: sha(v) for n, v in files.items()},
        'acknowledge_samples_redirected': ['ACK1', 'ACK2', 'ACK3'],
        'sequential_assignments_normalized': sequential,
        'start_handshake': 'wait END low after GO before accepting END high',
        'raw_sda_use': 'open-drain output only; input sampled through board synchronizer',
        'bus_clock_divider_and_register_values': 'unchanged; every LUT row is issued'}
