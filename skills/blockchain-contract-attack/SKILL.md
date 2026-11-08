---
name: blockchain-contract-attack
description: >-
  Blockchain/smart contracts: Etherscan, slither/mythril, reentrancy/access control/oracle/flash loan, cross-chain bridge, RPC exposure. Use when auditing smart contracts, DeFi, or blockchain attack surfaces.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Blockchain / Smart Contracts

```
=== Blockchain/smart contracts ===
Source code: Etherscan getsourcecode API | audit slither/mythril/manticore
Manual: reentrancy (.call{value} transferring before updating state violates checks-effects-interactions) | access control (missing onlyOwner) | integer overflow (<0.8 without SafeMath)
  Oracle manipulation (flash loan to instantaneously manipulate the AMM price) | randomness (block.timestamp is controllable) | authorization abuse (unlimited approve/permit replay) | delegatecall proxy storage collision
DeFi: flash-loan attack/sandwich (front-running)/governance attack/signature replay | cross-chain bridge: signature-threshold bypass/replay | RPC: exposed 8545 allows direct eth_sendTransaction
```
