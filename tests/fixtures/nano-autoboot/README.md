# Pinned native autoboot producer

`abortboot.c` is the unchanged `abortboot_single_key` function from
Terasic U-Boot `d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`,
`common/autoboot.c`, with its original license/copyright header.
SHA-256: `cea9669ec56a781fda8ee12f2739c594d2944866e56675dfb59b97ace84ddc7e`.

The host test supplies timer and keyboard callbacks, compiles the real function,
and checks its complete or interrupted wire against the selected native startup
policy. This is synthetic host evidence and grants no physical boot acceptance.
