# SPDX-License-Identifier: MIT
cmake_minimum_required(VERSION 3.16)
project(ethernetip_interop_eipscanner CXX)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
# EIPScanner's source tree, unpacked by the Dockerfile.
add_subdirectory(${EIPSCANNER_SOURCE} eipscanner EXCLUDE_FROM_ALL)
add_executable(ethernetip-interop src/main.cpp)
target_include_directories(ethernetip-interop PRIVATE ${EIPSCANNER_SOURCE}/src)
target_link_libraries(ethernetip-interop PRIVATE EIPScannerS pthread)
target_compile_options(ethernetip-interop PRIVATE -Wall -Wextra)
