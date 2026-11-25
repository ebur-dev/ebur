package share

import (
	"bytes"
	"os"
	"path/filepath"
)

// PluginName is the must-use plugin Ebur adds to shared WordPress sites.
const PluginName = "ebur-share.php"

// wpPlugin makes WordPress answer on the share address. It only acts on
// requests that came through an `ebur share` tunnel, which Nginx marks
// with EBUR_SHARE; normal visits on this computer are untouched. It never
// writes to the database or wp-config.php.
const wpPlugin = `<?php
/*
 * Plugin Name: Ebur share links
 * Description: Added by Ebur. Lets this site work through an "ebur share" address. It does nothing for normal visits and never changes the database. Safe to delete; Ebur adds it again on the next share.
 * Version: 1
 */
if (empty($_SERVER['EBUR_SHARE']) || empty($_SERVER['HTTP_HOST'])) {
    return;
}
$ebur_share = 'https://' . $_SERVER['HTTP_HOST'];
$ebur_local = rtrim((string) get_option('home'), '/');

// WordPress redirects to its saved address; for shared visits use the share address.
add_filter('option_home', function () use ($ebur_share) { return $ebur_share; });
add_filter('option_siteurl', function () use ($ebur_share) { return $ebur_share; });

// Links and images saved in posts still name the local address: rewrite them in the page.
if ($ebur_local !== '' && $ebur_local !== $ebur_share) {
    $ebur_host = preg_replace('#^https?://#', '', $ebur_local);
    $ebur_from = array('http://' . $ebur_host, 'https://' . $ebur_host, 'http:\/\/' . $ebur_host, 'https:\/\/' . $ebur_host, '//' . $ebur_host);
    $ebur_to = array($ebur_share, $ebur_share, str_replace('/', '\/', $ebur_share), str_replace('/', '\/', $ebur_share), $ebur_share);
    ob_start(function ($html) use ($ebur_from, $ebur_to) { return str_replace($ebur_from, $ebur_to, $html); });
}
`

// IsWordPress reports whether dir holds a WordPress install.
func IsWordPress(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "wp-includes", "version.php"))
	return err == nil
}

// EnsureWordPressPlugin adds or updates the plugin in a WordPress site.
// It reports whether it wrote the file. Non-WordPress sites are left alone.
func EnsureWordPressPlugin(dir string) (bool, error) {
	if !IsWordPress(dir) {
		return false, nil
	}
	mu := filepath.Join(dir, "wp-content", "mu-plugins")
	path := filepath.Join(mu, PluginName)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, []byte(wpPlugin)) {
		return false, nil
	}
	if err := os.MkdirAll(mu, 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(wpPlugin), 0o644)
}
