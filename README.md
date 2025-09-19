# **SoftRoom**

Simple SSH-based secure chat room server for groups. It have auth system based on GitHub OAuth and works directly in SSH terminal. Supported the private messages. The server have not any message logging system at all.

**WARNING!!! This project works without visible prolems but it's NOT FINISHED and WAS NOT TESTED WELL. Use with caution.**


## **Features**

* **SSH-Based:** Secure, encrypted, and accessible from any standard SSH client.  
* **Passwordless** Uses GitHub OAuth for login.  
* **Cross-Platform:** The server compiles and runs as a single binary on Linux, Windows, and FreeBSD.   
* **Slash Commands:** Includes support for private messaging and user commands.

## **Installation the server**

### **1. Compiling and configuring the server**

Just "cd" to the folder and run "go build". That's it. Run the executable file, it will create the config file with default settings. Change the settings in the config file and enjoy.

### **2. Create a GitHub OAuth App**

The server requires a GitHub OAuth App for user authentication.

1. Navigate to your GitHub **Settings** -> **Developer settings** -> **OAuth Apps**.  
2. Click **"New OAuth App"**.  
3. Fill the details:  
   * **Application name:** Name of your choice.
   * **Homepage URL:** http://localhost  
   * **Authorization callback URL:** http://localhost (This is just a placeholder and is not used by the application).  
4. Activate the checkbox **"Enable Device Flow"**.  
5. Click **"Register application"**.  
6. On the next page, generate a new **Client ID** and copy it.

You DON'T NEED the "Client secret" at all. Just put the "Client ID" to the config file.

## **How to Connect**

Connect to the server using any standard SSH client.

ssh <server_ip> -p <server_port>
Use port from the config file of the server.

Right after connecting to the server, you will get a link to the GitHub OAuth page and the code what you should use at the provided link. As soon as you apply the code and grant the access for the application to get your username, you will be logged into the chat room.

## **SSH Client Requirements**

Your SSH client must support pseudo-terminals (PTY), which is standard for most clients. Avoid to use Putty, it's prolematic and additional configuration is necessary.


## **Available Commands**

* /h: Show the help message with all available commands.  
* /u: List all users currently online in the chat.  
* /w <username> <message>: Send a private message to a specific user.
