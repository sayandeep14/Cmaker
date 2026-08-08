#include <iostream>

#include <opencv2/core.hpp>
#include <opencv2/imgproc.hpp>

int main() {
    // A synthetic image (a filled square on a black background) - no
    // external file needed to build/run this demo, and unlike a smooth
    // gradient, its actual edges give Canny something real to detect.
    cv::Mat image = cv::Mat::zeros(100, 100, CV_8UC1);
    cv::rectangle(image, cv::Point(25, 25), cv::Point(75, 75), cv::Scalar(255), cv::FILLED);

    cv::Mat edges;
    cv::Canny(image, edges, 50, 150);

    std::cout << "Hello from cmaker (opencv template)!\n";
    std::cout << "OpenCV version: " << CV_VERSION << "\n";
    std::cout << "Generated a " << image.rows << "x" << image.cols
              << " test image and ran Canny edge detection.\n";
    std::cout << "Edge pixel count: " << cv::countNonZero(edges) << "\n";
    return 0;
}
