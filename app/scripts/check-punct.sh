#!/bin/sh
# Fails when an em dash or middle dot is in the source.
! grep -rn "—\|·" src DESIGN.md PRODUCT.md 2>/dev/null
