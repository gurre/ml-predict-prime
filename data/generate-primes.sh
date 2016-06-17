#!/bin/bash

# Not used!!!

# We need them sorted.
if [[ $(hash primesieve) -eq 0 ]]; then
  #echo "Sieving primes 0-1e9"
  #primesieve 0 1e9 -t2 -s2048 --print=1 | sort -k 1,1n > data/prime-1e9.txt
  echo "Sieving twins 0-1e9"
  primesieve 0 1e9 -t2 -s2048 --print=2 | sort -k 2,2n > data/twin-1e9.txt
  echo "Sieving triplets 0-1e9"
  primesieve 0 1e9 -t2 -s2048 --print=3 | sort -k 2,2n > data/triplet-1e9.txt
  echo "Sieving quads 0-1e9"
  primesieve 0 1e9 -t2 -s2048 --print=4 | sort -k 2,2n > data/quad-1e9.txt
  echo "Sieving penta 0-1e9"
  primesieve 0 1e9 -t2 -s2048 --print=5 | sort -k 2,2n > data/penta-1e9.txt
  echo "Sieving sexy 0-1e9"
  primesieve 0 1e9 -t2 -s2048 --print=6 | sort -k 2,2n > data/sexy-1e9.txt
  echo "Generation complete"
else
  echo "Failed: primesieve not installed"
fi
