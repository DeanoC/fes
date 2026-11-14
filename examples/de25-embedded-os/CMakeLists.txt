cmake_minimum_required(VERSION 3.20.0)
find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})
project(fes_embedded_probe LANGUAGES C CXX)

target_sources(app PRIVATE src/probe.cpp src/selftest.cpp src/main.cpp)
target_include_directories(app PRIVATE src
  ${CMAKE_CURRENT_SOURCE_DIR}/../../sources/libmister-runtime/src)
target_compile_features(app PRIVATE cxx_std_14)
