#pragma once
#include "native/core_driver.hpp"
#include <string>
namespace mister { namespace native {
// The sealed splash has no user-I/O or HPS framebuffer.
struct IdleRecipe {
 std::string expected_core;
 ProgrammingProfile programming_profile = ProgrammingProfile::development_contained_v1;
};
inline IdleRecipe SplashIdle() { return {}; }
} }
