#include <gtkmm/application.h>
#include <gtkmm/button.h>
#include <gtkmm/window.h>

class HelloWindow : public Gtk::Window {
public:
    HelloWindow() {
        set_title("Cmaker GTKmm");
        set_default_size(400, 300);
        button.set_label("Hello from cmaker (gtkmm template)!");
        set_child(button);
    }

private:
    Gtk::Button button;
};

int main(int argc, char *argv[]) {
    auto app = Gtk::Application::create("dev.cmaker.gtkmm-template");
    return app->make_window_and_run<HelloWindow>(argc, argv);
}
