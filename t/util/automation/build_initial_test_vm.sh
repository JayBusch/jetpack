#!/bin/sh

downloadISO() {
    echo Downloading FreeBSD 10.2 ISO
    cd ./virtMachines/install_media
    fetch ftp://ftp.freebsd.org/pub/FreeBSD/releases/ISO-IMAGES/10.2/FreeBSD-10.2-RELEASE-amd64-bootonly.iso
    cd ..
    cd ..
}

createDiskImage() {
    echo Creating Disk Image
    truncate -s 10G ./virtMachines/freebsd_10_2_x64_bhyve/freebsd_10_2_x64_bhyve.img
}

installOS() {
    echo Installing OS
    osInstallCommand="sudo sh /usr/share/examples/bhyve/vmrun.sh -i -c 2 -m 1024M -t tap0 -d $PWD/virtMachines/freebsd_10_2_x64_bhyve/freebsd_10_2_x64_bhyve.img -I $PWD/virtMachines/install_media/FreeBSD-10.2-RELEASE-amd64-bootonly.iso freebsd_10_2_x64_bhyve"

    $osInstallCommand
}

installDependencies() {
    echo Installing Dependencies
}

provisionTestVM() {    
    createDiskImage
    installOS
    installDependencies
}

if ( ls ./virtMachines/install_media/FreeBSD-10.2-RELEASE-amd64-bootonly.iso 2>/dev/null )
then
    sha256 -c c19a48715b2bd42ac65af0db0509c3720d765e6badcebaa96c978897f6496409 ./virtMachines/install_media/FreeBSD-10.2-RELEASE-amd64-bootonly.iso
    if [ $? -eq 0 ];then
	echo Complete ISO File Found
    else
	downloadISO
    fi
else
    downloadISO
fi

provisionTestVM
