#include <iostream>

#include "oatpp/core/macro/component.hpp"
#include "oatpp/network/Server.hpp"
#include "oatpp/network/tcp/server/ConnectionProvider.hpp"
#include "oatpp/web/server/HttpConnectionHandler.hpp"
#include "oatpp/web/server/HttpRouter.hpp"

class Handler : public oatpp::web::server::HttpRequestHandler {
public:
    std::shared_ptr<OutgoingResponse> handle(const std::shared_ptr<IncomingRequest> &request) override {
        return ResponseFactory::createResponse(Status::CODE_200, "Hello from cmaker (oatpp template)!\n");
    }
};

int main() {
    oatpp::base::Environment::init();

    auto router = oatpp::web::server::HttpRouter::createShared();
    router->route("GET", "/", std::make_shared<Handler>());

    auto connectionProvider = oatpp::network::tcp::server::ConnectionProvider::createShared(
        {"127.0.0.1", 8080, oatpp::network::Address::IP_4});
    auto connectionHandler = oatpp::web::server::HttpConnectionHandler::createShared(router);

    oatpp::network::Server server(connectionProvider, connectionHandler);
    std::cout << "Listening on http://127.0.0.1:8080 (Ctrl+C to stop)\n";
    server.run();

    oatpp::base::Environment::destroy();
    return 0;
}
