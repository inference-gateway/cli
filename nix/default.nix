# Default entry point for nix-build
{ pkgs ? import <nixpkgs> { } }:

pkgs.callPackage ./package.nix { }
