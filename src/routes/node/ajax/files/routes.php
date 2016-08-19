<?php

/*
  PufferPanel - A Game Server Management Panel
  Copyright (c) 2015 Dane Everitt

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU General Public License for more details.

  You should have received a copy of the GNU General Public License
  along with this program.  If not, see http://www.gnu.org/licenses/.
 */

namespace PufferPanel\Core;

use \Unirest;

$klein->respond('POST', '/node/ajax/files/[*]', function() use($core) {

    $core->routes = new Router\Router_Controller('Node\Ajax\Files', $core->server);
    $core->routes = $core->routes->loadClass();
});

$klein->respond('POST', '/node/ajax/files/delete', function($request, $response) use($core) {

    if (!$core->permissions->has('files.delete')) {

        $response->code(403)->body("You are not authorized to perform this action.")->send();
        return;
    }

    $unirest = (object) array("code" => 500, "raw_body" => "Invalid function passed.");

    if ($request->param('deleteItemPath') && !empty($request->param('deleteItemPath'))) {

        try {

            $bearer = OAuthService::Get()->getPanelAccessToken();
            $header = array(
                'Authorization' => 'Basic ' . $bearer
            );

            $unirest = Unirest\Request::delete(sprintf('https://%s:%s/server/%s/file/%s', array(
                        $core->server->nodeData('fqdn'),
                        $core->server->nodeData('daemon_listen'),
                        $core->server->getData('hash'),
                        rawurlencode($request->param('deleteItemPath')))), $header
            );
        } catch (\Exception $e) {

            \Tracy\Debugger::log($e);
            $response->code(500)->body("Unirest response error.")->send();
            return;
        }
    }

    if ($unirest->code !== 204) {

        $response->code($unirest->code)->body($unirest->raw_body)->send();
        return;
    }

    $response->code(200)->body("ok")->send();
});

$klein->respond('POST', '/node/ajax/files/save', function($request, $response) use($core) {

    if (!$core->permissions->has('files.save')) {

        $response->code(403)->body("You are not authorized to perform this action.")->send();
        return;
    }

    if (!$core->auth->XSRF($request->param('xsrf'))) {

        $response->code(403)->body('The XSRF token recieved was not valid. Please make sure cookies are enabled and try your request again.')->send();
        return;
    }

    if (!$request->param('file') || !$request->param('file_contents')) {

        $response->code(500)->body('Not all required parameters were passed to the script.')->send();
        return;
    }

    $file = (object) pathinfo($request->param('file'));

    if (!in_array($file->extension, $core->files->editable())) {

        $response->code(500)->body("This type of file cannot be edited.")->send();
        return;
    }

    if (in_array($file->dirname, array(".", "./", "/"))) {
        $file->dirname = "";
    } else {
        $file->dirname = trim($file->dirname, '/') . "/";
    }

    try {

        $bearer = OAuthService::Get()->getPanelAccessToken();
        $header = array(
            'Authorization' => 'Basic ' . $bearer
        );

        $unirest = Unirest\Request::put(sprintf('https://%s:%s/server/%s/file/%s', array(
                    $core->server->nodeData('fqdn'),
                    $core->server->nodeData('daemon_listen'),
                    $core->server->getData('hash'),
                    rawurlencode($file->dirname . $file->basename))), $header, $request->param('file_contents')
        );

        if ($unirest->code === 204) {

            $response->body('<div class="alert alert-success">File has been successfully saved.</div>')->send();
        } else {

            $response->code(500)->body("An error occured while trying to save this file. [" . $unirest->body->message . "]")->send();
        }
    } catch (\Exception $e) {

        \Tracy\Debugger::log($e);
        $response->code(500)->body("An error was encountered when trying to connect to the remote server to save this file.")->send();
    }
});

$klein->respond('POST', '/node/ajax/files/directory', function($request, $response) use($core) {

    if (!$core->permissions->has('files.view')) {

        $response->code(403);
        $response->body("You are not authorized to perform this action.")->send();
        return;
    }

    $previous_directory = array();
    if (!empty($request->param('dir'))) {
        $previous_directory['first'] = true;
    }

    $go_back = explode('/', ltrim(rtrim($request->param('dir'), '/'), '/'));
    if (count($go_back) > 1 && !empty($go_back[1])) {

        $previous_directory['show'] = true;
        $previous_directory['link'] = trim(str_replace(end($go_back), "", trim(implode('/', $go_back), '/')), '/');
        $previous_directory['link_show'] = rtrim($previous_directory['link'], "/");
    }

    if (!$core->routes->buildContents($request->params())) {

        $response->body($core->routes->retrieveLastError())->send();
    } else {

        $response->body($core->twig->render('node/files/ajax/list_dir.html', array(
                    'files' => $core->routes->getFiles(),
                    'folders' => $core->routes->getFolders(),
                    'extensions' => $core->files->editable(),
                    'zip_extensions' => array('zip', 'tar.gz', 'tar', 'gz'),
                    'directory' => $previous_directory,
                    'header_dir' => ($request->param('dir')) ? trim($request->param('dir'), '/') . "/" : null
        )))->send();
    }
});
