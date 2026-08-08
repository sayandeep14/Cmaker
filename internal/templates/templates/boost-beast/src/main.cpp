#include <iostream>

#include <boost/asio/ip/tcp.hpp>
#include <boost/beast/core.hpp>
#include <boost/beast/http.hpp>

namespace beast = boost::beast;
namespace http = beast::http;
namespace net = boost::asio;
using tcp = net::ip::tcp;

int main() {
    try {
        net::io_context ioc{1};
        tcp::acceptor acceptor{ioc, {net::ip::make_address("127.0.0.1"), 8080}};
        std::cout << "Listening on http://127.0.0.1:8080 (Ctrl+C to stop)\n";

        for (;;) {
            tcp::socket socket{ioc};
            acceptor.accept(socket);

            beast::flat_buffer buffer;
            http::request<http::string_body> req;
            http::read(socket, buffer, req);

            http::response<http::string_body> res{http::status::ok, req.version()};
            res.set(http::field::server, "cmaker-boost-beast-template");
            res.set(http::field::content_type, "text/plain");
            res.body() = "Hello from cmaker (boost-beast template)!\n";
            res.prepare_payload();

            http::write(socket, res);
            socket.shutdown(tcp::socket::shutdown_send);
        }
    } catch (const std::exception &e) {
        std::cerr << "Error: " << e.what() << "\n";
        return 1;
    }
    return 0;
}
