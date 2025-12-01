# Default entry point for nix-build
{ pkgs ? import <nixpkgs> { } }:

pkgs.callPackage ./infer.nix { }
