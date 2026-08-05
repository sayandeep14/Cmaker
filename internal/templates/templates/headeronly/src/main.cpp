#include <iostream>

#include "mathutils.hpp"

int main() {
    std::cout << "2 + 3 = " << mathutils::add(2, 3) << std::endl;
    std::cout << "4^2 = " << mathutils::square(4) << std::endl;
    return 0;
}
