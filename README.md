## Function

This script does the following for each of your GCP Compute instances:

1. Get the PTR record configuration (in network interface tab)
2. Get its current IP, or nothing if it is powered off.
3. Create the corrosponding DNS records in your cloudflare zone based on the domain name in the PTR, and the IP of the instance, or delete the record if the machine is off.

## Usage

1. Create a file named something like `cloudflare-cred.ini`, and populate it with the following:

<pre>
dns_cloudflare_api_token = <em style="font-weight: bold;">your cloudflare API token</em>
</pre>

2. Create a service account with the "Compute Viewer" role, and download their GCP credential to a JSON file.

3. Run the script with the following environment variables:

<pre>
CLOUDFLARE_INI=<em style="font-weight: bold;">location of the file created in step 1</em>
GOOGLE_APPLICATION_CREDENTIALS=<em style="font-weight: bold;">location of the file created in step 2</em>
</pre>
