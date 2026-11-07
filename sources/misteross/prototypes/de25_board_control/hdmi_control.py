"""Qualify the captured HDMI interrupt and wait for actual I2C busy entry."""
import hashlib
from pathlib import Path

BOARD_SOURCE = 'sources/misteross/boards/de25nano/zx81-hps/board.v'
BOARD_SHA256 = 'a0d7f396a31746a1fa6a977f24d9aeef09a2f38a68537a50f85e7524ea08ff0b'
CONFIG_SHA256 = 'b9b7c477173fee9b6cb5e3d6a6ba1122c7a0b539f205d807b1d61b22048fd391'
IRQ = b'''    // INT is an asynchronous, active-low level. Its first stage is the
    // only raw-pin sink; both stages share the existing qualified 50 MHz reset.
    // The vendor's divided-clock consumer remains a timed, related-clock path.
    (* preserve, dont_merge *) reg [1:0] hdmi_irq_sync;
    always @(posedge CLOCK0_50 or negedge config_reset_n) begin
        if (!config_reset_n) hdmi_irq_sync <= 2'b11;
        else hdmi_irq_sync <= {hdmi_irq_sync[0], HDMI_TX_INT};
    end
'''


def board(source):
    if hashlib.sha256(source).hexdigest() != BOARD_SHA256:
        raise ValueError('captured HDMI board wrapper changed; review interrupt ownership')
    anchor = b'    I2C_HDMI_Config hdmi_config '
    connection = b'.HDMI_TX_INT(HDMI_TX_INT)'
    if source.count(anchor) != 1 or source.count(connection) != 1:
        raise ValueError('captured HDMI interrupt connection changed')
    return source.replace(anchor, IRQ + anchor).replace(connection, b'.HDMI_TX_INT(hdmi_irq_sync[1])')


def configuration(source):
    if hashlib.sha256(source).hexdigest() != CONFIG_SHA256:
        raise ValueError('captured HDMI initializer changed; review GO/busy/completion handshake')
    before = b'mSetup_ST\t<=\t1;\r\n\t\t\t\tend\r\n\t\t\t1:\tbegin'
    after = (b'mSetup_ST\t<=\t3;\r\n\t\t\t\tend\r\n'
             b'\t\t\t// END may still describe the previous transfer. Keep GO\r\n'
             b'\t\t\t// asserted until this controller has actually entered busy.\r\n'
             b'\t\t\t3: if (!mI2C_END) mSetup_ST <= 1;\r\n'
             b'\t\t\t1:\tbegin')
    if source.count(before)!=1:raise ValueError('HDMI initializer handshake anchor changed')
    return source.replace(before,after)


def install(project, inputs, sources):
    path = Path(project)/'fes_mailbox/board.v'
    original = sources[BOARD_SOURCE]
    if path.read_bytes() != original:
        raise ValueError('HDMI wrapper differs before interrupt qualification')
    derived = board(original)
    config=Path(project)/'fes_mailbox/I2C_HDMI_Config.v'
    original_config=config.read_bytes();derived_config=configuration(original_config)
    path.write_bytes(derived)
    inputs['computer']['source_sha256']['board.v'] = hashlib.sha256(derived).hexdigest()
    config.with_suffix('.v.original').write_bytes(original_config)
    config.write_bytes(derived_config)
    inputs['computer']['source_sha256']['I2C_HDMI_Config.v']=hashlib.sha256(derived_config).hexdigest()


def verify(project, inputs, sources):
    expected = board(sources[BOARD_SOURCE])
    if ((Path(project)/'fes_mailbox/board.v').read_bytes() != expected or
            inputs['computer']['source_sha256'].get('board.v') != hashlib.sha256(expected).hexdigest()):
        raise ValueError('HDMI interrupt wrapper differs from its captured source and owner')
    config=Path(project)/'fes_mailbox/I2C_HDMI_Config.v'
    expected=configuration(config.with_suffix('.v.original').read_bytes())
    if config.read_bytes()!=expected or inputs['computer']['source_sha256'].get(config.name)!=hashlib.sha256(expected).hexdigest():
        raise ValueError('HDMI initializer differs from its captured source and busy handshake')
