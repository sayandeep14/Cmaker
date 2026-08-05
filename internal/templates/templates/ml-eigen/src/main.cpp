#include <Eigen/Dense>
#include <iostream>

int main() {
    std::cout << "Hello from cmaker (ml-eigen template)!\n";

    // A tiny "ML-adjacent" numerics demo: solve a 2x2 linear system Ax = b,
    // the kind of building block real ML/numerics code layers on top of.
    Eigen::Matrix2f A;
    A << 2, 1,
         1, 3;
    Eigen::Vector2f b(3, 5);
    Eigen::Vector2f x = A.colPivHouseholderQr().solve(b);

    std::cout << "Solving A x = b for:\nA =\n" << A << "\nb =\n" << b << "\n";
    std::cout << "x =\n" << x << "\n";
    return 0;
}
