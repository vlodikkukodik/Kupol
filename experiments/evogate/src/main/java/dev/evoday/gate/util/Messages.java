package dev.evoday.gate.util;

import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.minimessage.MiniMessage;
import net.kyori.adventure.text.minimessage.tag.resolver.Placeholder;
import net.kyori.adventure.text.minimessage.tag.resolver.TagResolver;
import org.bukkit.command.CommandSender;
import org.bukkit.configuration.file.YamlConfiguration;
import org.bukkit.plugin.java.JavaPlugin;

import java.io.File;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;

// плейсхолдеры парами: "name", value, ...
public final class Messages {

    private static final MiniMessage MM = MiniMessage.miniMessage();

    private final JavaPlugin plugin;
    private YamlConfiguration file;
    private YamlConfiguration defaults;

    public Messages(JavaPlugin plugin) {
        this.plugin = plugin;
        reload();
    }

    public void reload() {
        File target = new File(plugin.getDataFolder(), "messages.yml");
        if (!target.exists()) {
            plugin.saveResource("messages.yml", false);
        }
        file = YamlConfiguration.loadConfiguration(target);
        var in = plugin.getResource("messages.yml");
        if (in != null) {
            defaults = YamlConfiguration.loadConfiguration(new InputStreamReader(in, StandardCharsets.UTF_8));
            file.setDefaults(defaults);
            // новые сообщения из jar дописываются в messages.yml
            boolean added = false;
            for (String key : defaults.getKeys(false)) {
                if (!file.isSet(key)) {
                    file.set(key, defaults.get(key));
                    added = true;
                }
            }
            if (added) {
                try {
                    file.save(target);
                } catch (java.io.IOException e) {
                    plugin.getLogger().warning("Can't update messages.yml: " + e.getMessage());
                }
            }
        }
    }

    public Component raw(String key, Object... placeholders) {
        String text = file.getString(key, key);
        return MM.deserialize(text, resolver(placeholders));
    }

    public Component get(String key, Object... placeholders) {
        return MM.deserialize(file.getString("prefix", "")).append(raw(key, placeholders));
    }

    public void send(CommandSender to, String key, Object... placeholders) {
        to.sendMessage(get(key, placeholders));
    }

    private static TagResolver resolver(Object... placeholders) {
        TagResolver.Builder builder = TagResolver.builder();
        for (int i = 0; i + 1 < placeholders.length; i += 2) {
            builder.resolver(Placeholder.unparsed(String.valueOf(placeholders[i]), String.valueOf(placeholders[i + 1])));
        }
        return builder.build();
    }
}
