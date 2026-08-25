cmake_minimum_required(VERSION 3.20)
project(acme_consumer LANGUAGES CXX)

add_executable(acme-consumer main.cpp)
target_link_libraries(acme-consumer PRIVATE acme::lib-utils)
